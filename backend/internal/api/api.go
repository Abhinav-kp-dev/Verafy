package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/time/rate"

	"coveragecheck/internal/chatbot"
	"coveragecheck/internal/config"
	"coveragecheck/internal/db"
	"coveragecheck/internal/events"
	"coveragecheck/internal/llm"
	"coveragecheck/internal/notify"
	"coveragecheck/internal/queue"
	"coveragecheck/internal/ratelimit"
)

type Server struct {
	cfg     *config.Config
	store   *db.Store
	queue   *queue.Queue
	hub     *events.Hub
	limiter *ratelimit.PayerLimiter
	llm     *llm.Generator
	sender  notify.Sender
	log     *slog.Logger
	stediMode string

	practice  *db.Practice
	assistant *chatbot.Assistant
	chatLim   *chatLimiter

	ipMu   sync.Mutex
	ipLims map[string]*rate.Limiter
}

func New(cfg *config.Config, store *db.Store, q *queue.Queue, hub *events.Hub, lim *ratelimit.PayerLimiter, gen *llm.Generator, sender notify.Sender, assistant *chatbot.Assistant, stediMode string, log *slog.Logger) (*Server, error) {
	p, err := store.DefaultPractice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("no practice seeded — run `go run ./cmd/seed` first: %w", err)
	}
	return &Server{cfg: cfg, store: store, queue: q, hub: hub, limiter: lim, llm: gen, sender: sender, assistant: assistant, stediMode: stediMode, log: log, practice: p,
		chatLim: newChatLimiter(cfg.ChatRPM, cfg.ChatBurst), ipLims: map[string]*rate.Limiter{}}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/config", s.configInfo)
	mux.HandleFunc("GET /api/payers", s.listPayers)
	mux.HandleFunc("GET /api/patients", s.listPatients)
	mux.HandleFunc("POST /api/patients", s.createPatient)
	mux.HandleFunc("DELETE /api/patients", s.deletePatients)
	mux.HandleFunc("GET /api/patients/{id}", s.getPatient)
	mux.HandleFunc("POST /api/verifications", s.createVerification)
	mux.HandleFunc("GET /api/verifications", s.listVerifications)
	mux.HandleFunc("DELETE /api/verifications", s.deleteVerifications)
	mux.HandleFunc("GET /api/verifications/{id}", s.getVerification)
	mux.HandleFunc("POST /api/batches", s.createBatch)
	mux.HandleFunc("POST /api/batches/csv", s.createBatchCSV)
	mux.HandleFunc("GET /api/batches/csv-template", s.csvTemplate)
	mux.HandleFunc("POST /api/batches/synthetic", s.createSyntheticBatch)
	mux.HandleFunc("GET /api/batches", s.listBatches)
	mux.HandleFunc("GET /api/batches/{id}", s.getBatch)
	mux.HandleFunc("GET /api/review", s.listReview)
	mux.HandleFunc("POST /api/review/{id}/resolve", s.resolveReview)
	mux.HandleFunc("GET /api/stats", s.stats)
	mux.HandleFunc("GET /api/ratelimits", s.rateLimits)
	mux.Handle("GET /api/events", s.hub)
	s.registerPrevisitRoutes(mux)
	s.registerChatRoutes(mux)
	s.registerControlRoutes(mux)
	s.registerPDFRoutes(mux)
	s.registerNotificationRoutes(mux)
	s.registerVoiceRoutes(mux)
	return s.cors(s.inboundLimit(mux))
}

// ---------- middleware ----------

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == s.cfg.CORSOrigin || strings.HasPrefix(origin, "http://localhost:") || strings.HasPrefix(origin, "http://127.0.0.1:") {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// inboundLimit: 30 req/s per client IP (burst 60). SSE is exempt.
func (s *Server) inboundLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/events") {
			next.ServeHTTP(w, r)
			return
		}
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		s.ipMu.Lock()
		l, ok := s.ipLims[ip]
		if !ok {
			l = rate.NewLimiter(30, 60)
			s.ipLims[ip] = l
		}
		s.ipMu.Unlock()
		if !l.Allow() {
			w.Header().Set("Retry-After", "1")
			writeErr(w, http.StatusTooManyRequests, "Too many requests — please wait a moment")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 8<<20)).Decode(v)
}

func parseID(r *http.Request) (uuid.UUID, error) { return uuid.Parse(r.PathValue("id")) }

func parseDOB(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2006-01-02", "01/02/2006", "1/2/2006", "2006/01/02", "02-01-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid date %q (use YYYY-MM-DD)", s)
}

func qInt(r *http.Request, k string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(k)); err == nil {
		return v
	}
	return def
}

// ---------- handlers ----------

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true, "stediMode": s.stediMode, "time": time.Now()})
}

func (s *Server) configInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"practice":     s.practice,
		"stediMode":    s.stediMode,
		"stediBaseURL": s.cfg.StediBaseURL,
		"providerNPI":  s.cfg.ProviderNPI,
		"providerName": s.cfg.ProviderName,
		"llmEnabled":   s.llm.Enabled(),
		"llmModel":     s.cfg.LLMModel,
		"maxWorkers":   s.cfg.MaxWorkers,
		"maxAttempts":  s.cfg.MaxAttempts,
		"retryBase":    s.cfg.RetryBase.String(),
		"payerRPS":     s.cfg.PayerRPS,
		"payerBurst":   s.cfg.PayerBurst,
		"chatEnabled":   s.assistant != nil && s.assistant.Configured(),
		"emailDelivery": s.sender != nil && s.sender.Configured(),
		"emailProvider": s.emailProvider(),
		"nightlyHour":   s.cfg.NightlyHour,
		"timezone":      s.cfg.Timezone,
		"practicePhone": s.cfg.PracticePhone,
		"voiceMode":     s.cfg.VoiceMode,
		"voiceProvider": s.cfg.VoiceProvider,
		"voiceCallTimeout": s.cfg.VoiceCallTimeout.String(),
	})
}

func (s *Server) listPayers(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.ListPayers(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) listPatients(w http.ResponseWriter, r *http.Request) {
	p, err := s.store.ListPatients(r.Context(), r.URL.Query().Get("search"), qInt(r, "limit", 200))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) deletePatients(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if len(in.IDs) == 0 {
		writeErr(w, 400, "ids is required and must be non-empty")
		return
	}
	if len(in.IDs) > 1000 {
		writeErr(w, 400, "at most 1000 ids per request")
		return
	}
	ids := make([]uuid.UUID, 0, len(in.IDs))
	for _, s := range in.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			writeErr(w, 400, "invalid id: "+s)
			return
		}
		ids = append(ids, id)
	}
	n, err := s.store.DeletePatients(r.Context(), ids)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]int{"deleted": n})
}

func (s *Server) getPatient(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	p, err := s.store.GetPatient(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "patient not found")
		return
	}
	jobs, _, _ := s.store.ListJobs(r.Context(), db.JobFilter{Search: "", Limit: 20})
	own := []db.Job{}
	for _, j := range jobs {
		if j.PatientID == id {
			own = append(own, j)
		}
	}
	writeJSON(w, 200, map[string]any{"patient": p, "recentJobs": own})
}

type patientInput struct {
	Name     string `json:"name"`
	DOB      string `json:"dob"`
	MemberID string `json:"memberId"`
	PayerID  string `json:"payerId"`
	Email    string `json:"email"` // optional
	Phone    string `json:"phone"` // optional
}

func (s *Server) createPatient(w http.ResponseWriter, r *http.Request) {
	var in patientInput
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	np, err := s.toNewPatient(in)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	id, err := s.store.CreatePatient(r.Context(), np)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	p, _ := s.store.GetPatient(r.Context(), id)
	writeJSON(w, 201, p)
}

func (s *Server) toNewPatient(in patientInput) (db.NewPatient, error) {
	if strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.MemberID) == "" {
		return db.NewPatient{}, errors.New("name and memberId are required")
	}
	dob, err := parseDOB(in.DOB)
	if err != nil {
		return db.NewPatient{}, err
	}
	pid, err := uuid.Parse(in.PayerID)
	if err != nil {
		return db.NewPatient{}, errors.New("payerId must be a valid payer")
	}
	email := strings.TrimSpace(in.Email)
	if email != "" && (!strings.Contains(email, "@") || strings.ContainsAny(email, " \t\n")) {
		return db.NewPatient{}, errors.New("email address looks invalid")
	}
	return db.NewPatient{PracticeID: s.practice.ID, Name: strings.TrimSpace(in.Name), DOB: dob, MemberID: strings.TrimSpace(in.MemberID), PayerID: pid, Email: email, Phone: strings.TrimSpace(in.Phone)}, nil
}

// createVerification: single real-time check for an existing patient, or ad-hoc details.
func (s *Server) createVerification(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PatientID string `json:"patientId"`
		patientInput
		SavePatient bool `json:"savePatient"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	ctx := r.Context()
	var patientID uuid.UUID
	if in.PatientID != "" {
		id, err := uuid.Parse(in.PatientID)
		if err != nil {
			writeErr(w, 400, "bad patientId")
			return
		}
		patientID = id
	} else {
		np, err := s.toNewPatient(in.patientInput)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		id, err := s.store.UpsertPatient(ctx, np)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		patientID = id
	}
	patient, err := s.store.GetPatient(ctx, patientID)
	if err != nil {
		writeErr(w, 404, "patient not found")
		return
	}
	ids, err := s.enqueueJobs(ctx, nil, []db.Patient{*patient}, false, queue.PriorityInteractive)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	job, _ := s.store.GetJob(ctx, ids[0])
	writeJSON(w, http.StatusAccepted, map[string]any{"jobId": ids[0], "job": job, "queued": true})
}

// enqueueJobs inserts domain jobs + River jobs in ONE transaction. Returns job IDs.
func (s *Server) enqueueJobs(ctx context.Context, batchID *uuid.UUID, patients []db.Patient, synthetic bool, priority int) ([]uuid.UUID, error) {
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	pairs := make([]struct{ PatientID, PayerID uuid.UUID }, len(patients))
	for i, p := range patients {
		pairs[i] = struct{ PatientID, PayerID uuid.UUID }{p.ID, p.PayerID}
	}
	ids, err := s.store.BulkInsertJobs(ctx, tx, batchID, pairs)
	if err != nil {
		return nil, err
	}
	if err := s.queue.Enqueue(ctx, tx, ids, synthetic, priority); err != nil {
		return nil, err
	}
	return ids, tx.Commit(ctx)
}

func (s *Server) newBatchTx(ctx context.Context, label, kind string, patients []db.Patient, synthetic bool) (uuid.UUID, error) {
	priority := queue.PriorityBatch
	if synthetic {
		priority = queue.PrioritySynthetic
	}
	tx, err := s.store.Pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)
	batchID, err := s.store.CreateBatch(ctx, tx, s.practice.ID, label, kind, len(patients))
	if err != nil {
		return uuid.Nil, err
	}
	pairs := make([]struct{ PatientID, PayerID uuid.UUID }, len(patients))
	for i, p := range patients {
		pairs[i] = struct{ PatientID, PayerID uuid.UUID }{p.ID, p.PayerID}
	}
	ids, err := s.store.BulkInsertJobs(ctx, tx, &batchID, pairs)
	if err != nil {
		return uuid.Nil, err
	}
	if err := s.queue.Enqueue(ctx, tx, ids, synthetic, priority); err != nil {
		return uuid.Nil, err
	}
	return batchID, tx.Commit(ctx)
}

func (s *Server) createBatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		PatientIDs []string `json:"patientIds"`
		All        bool     `json:"all"`
		Label      string   `json:"label"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	ctx := r.Context()
	var patients []db.Patient
	if in.All {
		var err error
		patients, err = s.store.ListPatients(ctx, "", 100000)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	} else {
		for _, sid := range in.PatientIDs {
			id, err := uuid.Parse(sid)
			if err != nil {
				continue
			}
			if p, err := s.store.GetPatient(ctx, id); err == nil {
				patients = append(patients, *p)
			}
		}
	}
	if len(patients) == 0 {
		writeErr(w, 400, "no patients selected")
		return
	}
	if in.Label == "" {
		in.Label = fmt.Sprintf("Batch of %d — %s", len(patients), time.Now().Format("Jan 2 15:04"))
	}
	batchID, err := s.newBatchTx(ctx, in.Label, "batch", patients, false)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	b, _ := s.store.GetBatch(ctx, batchID)
	writeJSON(w, http.StatusAccepted, b)
}

func (s *Server) csvTemplate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="coveragecheck_template.csv"`)
	w.Write([]byte("name,dob,member_id,payer,email,phone\nFalcon Dent,1985-06-07,007007007,AMTAS00425,patient@example.com,\nJaguar Dent,1996-05-05,U3141592653,Cigna Dental,,\n"))
}

// createBatchCSV accepts multipart "file": name,dob,member_id,payer (payer = Stedi payer ID or payer name).
func (s *Server) createBatchCSV(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		writeErr(w, 400, "expected multipart form with a CSV file")
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		writeErr(w, 400, "file field missing")
		return
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		writeErr(w, 400, "could not parse CSV: "+err.Error())
		return
	}
	if len(rows) < 2 {
		writeErr(w, 400, "CSV has no data rows")
		return
	}
	ctx := r.Context()
	payers, _ := s.store.ListPayers(ctx)
	byKey := map[string]db.Payer{}
	for _, p := range payers {
		byKey[strings.ToLower(p.StediPayerID)] = p
		byKey[strings.ToLower(p.Name)] = p
	}
	head := map[string]int{}
	for i, h := range rows[0] {
		head[strings.ToLower(strings.TrimSpace(h))] = i
	}
	col := func(row []string, names ...string) string {
		for _, n := range names {
			if i, ok := head[n]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
		}
		return ""
	}
	var patients []db.Patient
	problems := []string{}
	for i, row := range rows[1:] {
		name := col(row, "name", "patient", "patient_name")
		if name == "" {
			fn, ln := col(row, "first_name", "firstname"), col(row, "last_name", "lastname")
			name = strings.TrimSpace(fn + " " + ln)
		}
		dobStr := col(row, "dob", "date_of_birth", "dateofbirth")
		member := col(row, "member_id", "memberid", "member")
		email := col(row, "email", "email_address")
		phone := col(row, "phone", "phone_number")
		payerKey := strings.ToLower(col(row, "payer", "payer_id", "payerid", "insurance"))
		payer, ok := byKey[payerKey]
		dob, derr := parseDOB(dobStr)
		if name == "" || member == "" || !ok || derr != nil {
			problems = append(problems, fmt.Sprintf("row %d: name=%q member=%q payer=%q dob=%q", i+2, name, member, payerKey, dobStr))
			continue
		}
		id, err := s.store.UpsertPatient(ctx, db.NewPatient{PracticeID: s.practice.ID, Name: name, DOB: dob, MemberID: member, PayerID: payer.ID, Email: email, Phone: phone})
		if err != nil {
			problems = append(problems, fmt.Sprintf("row %d: %v", i+2, err))
			continue
		}
		patients = append(patients, db.Patient{ID: id, PayerID: payer.ID})
	}
	if len(patients) == 0 {
		writeJSON(w, 400, map[string]any{"error": "no valid rows", "problems": problems})
		return
	}
	batchID, err := s.newBatchTx(ctx, hdr.Filename, "csv", patients, false)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	b, _ := s.store.GetBatch(ctx, batchID)
	writeJSON(w, http.StatusAccepted, map[string]any{"batch": b, "skipped": problems})
}

// createSyntheticBatch: N jobs cycling over existing patients (reuses valid mock records).
func (s *Server) createSyntheticBatch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Count int `json:"count"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if in.Count <= 0 {
		in.Count = 1000
	}
	if in.Count > 50000 {
		in.Count = 50000
	}
	ctx := r.Context()
	base, err := s.store.ListPatients(ctx, "", 1000)
	if err != nil || len(base) == 0 {
		writeErr(w, 500, "no patients available to cycle")
		return
	}
	// Weight toward clean records so the mix resembles a real schedule:
	// ~94% real-time-supported, non-error subscribers; ~6% edge cases (AAA errors, unsupported payer).
	var clean, edge []db.Patient
	payers, _ := s.store.ListPayers(ctx)
	realtime := map[uuid.UUID]bool{}
	for _, p := range payers {
		realtime[p.ID] = p.SupportsRealtime
	}
	for _, p := range base {
		if realtime[p.PayerID] && !strings.HasPrefix(strings.ToUpper(p.MemberID), "UHCAAA") {
			clean = append(clean, p)
		} else {
			edge = append(edge, p)
		}
	}
	if len(clean) == 0 {
		clean = base
	}
	patients := make([]db.Patient, in.Count)
	for i := range patients {
		if len(edge) > 0 && i%16 == 15 {
			patients[i] = edge[(i/16)%len(edge)]
		} else {
			patients[i] = clean[i%len(clean)]
		}
	}
	batchID, err := s.newBatchTx(ctx, fmt.Sprintf("Synthetic load — %d jobs", in.Count), "synthetic", patients, true)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	b, _ := s.store.GetBatch(ctx, batchID)
	writeJSON(w, http.StatusAccepted, b)
}

func (s *Server) listBatches(w http.ResponseWriter, r *http.Request) {
	b, err := s.store.ListBatches(r.Context(), qInt(r, "limit", 20))
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, b)
}

func (s *Server) getBatch(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	b, err := s.store.GetBatch(r.Context(), id)
	if err != nil {
		writeErr(w, 404, "batch not found")
		return
	}
	jobs, total, err := s.store.ListJobs(r.Context(), db.JobFilter{BatchID: &id, Status: r.URL.Query().Get("status"), Limit: qInt(r, "limit", 100), Offset: qInt(r, "offset", 0)})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"batch": b, "jobs": jobs, "total": total})
}

func (s *Server) listVerifications(w http.ResponseWriter, r *http.Request) {
	f := db.JobFilter{Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("search"), Limit: qInt(r, "limit", 50), Offset: qInt(r, "offset", 0)}
	if b := r.URL.Query().Get("batchId"); b != "" {
		if id, err := uuid.Parse(b); err == nil {
			f.BatchID = &id
		}
	}
	if p := r.URL.Query().Get("payerId"); p != "" {
		if id, err := uuid.Parse(p); err == nil {
			f.Payer = &id
		}
	}
	jobs, total, err := s.store.ListJobs(r.Context(), f)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": jobs, "total": total})
}

func (s *Server) deleteVerifications(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if len(in.IDs) == 0 {
		writeErr(w, 400, "ids is required and must be non-empty")
		return
	}
	if len(in.IDs) > 1000 {
		writeErr(w, 400, "at most 1000 ids per request")
		return
	}
	ids := make([]uuid.UUID, 0, len(in.IDs))
	for _, s := range in.IDs {
		id, err := uuid.Parse(s)
		if err != nil {
			writeErr(w, 400, "invalid id: "+s)
			return
		}
		ids = append(ids, id)
	}
	n, err := s.store.DeleteJobs(r.Context(), ids)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]int{"deleted": n})
}

func (s *Server) getVerification(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	job, err := s.store.GetJob(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeErr(w, 404, "not found")
			return
		}
		writeErr(w, 500, err.Error())
		return
	}
	attempts, _ := s.store.ListAttempts(r.Context(), id)
	writeJSON(w, 200, map[string]any{"job": job, "attempts": attempts})
}

func (s *Server) listReview(w http.ResponseWriter, r *http.Request) {
	jobs, total, err := s.store.ListJobs(r.Context(), db.JobFilter{Status: string(db.StatusNeedsReview), Search: r.URL.Query().Get("search"), Limit: qInt(r, "limit", 200)})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": jobs, "total": total})
}

func (s *Server) resolveReview(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeErr(w, 400, "bad id")
		return
	}
	var in struct {
		Outcome    string `json:"outcome"` // verified | requeue
		ResolvedBy string `json:"resolvedBy"`
		Note       string `json:"note"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, "invalid JSON")
		return
	}
	if in.ResolvedBy == "" {
		in.ResolvedBy = "Front desk"
	}
	ctx := r.Context()
	switch in.Outcome {
	case "requeue":
		if err := s.store.RequeueFromReview(ctx, id); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
		tx, err := s.store.Pool.Begin(ctx)
		if err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		if err := s.queue.Enqueue(ctx, tx, []uuid.UUID{id}, false, queue.PriorityInteractive); err != nil {
			tx.Rollback(ctx)
			writeErr(w, 500, err.Error())
			return
		}
		tx.Commit(ctx)
	default:
		brief, _ := json.Marshal(map[string]any{
			"status": "active", "source": "manual", "brief": "Manually verified by " + in.ResolvedBy + ". " + in.Note,
			"summary": "Manually verified", "validation": "human", "flags": []string{"manually_verified"},
		})
		if err := s.store.MarkResolved(ctx, id, in.ResolvedBy, in.Note, brief); err != nil {
			writeErr(w, 409, err.Error())
			return
		}
	}
	job, _ := s.store.GetJob(ctx, id)
	writeJSON(w, 200, job)
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) rateLimits(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.limiter.Snapshot())
}

func (s *Server) emailProvider() string {
	switch {
	case s.cfg.ResendAPIKey != "":
		return "resend"
	case s.cfg.SMTPHost != "":
		return "smtp (" + s.cfg.SMTPHost + ")"
	}
	return "none"
}
