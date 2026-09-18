package api

import (
	"fmt"
	"net/http"
	"strings"

	"coveragecheck/internal/db"
	"coveragecheck/internal/pdfreport"
)

func (s *Server) registerPDFRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/patients/{id}/pdf", s.patientPDF)
	mux.HandleFunc("GET /api/reports/pdf", s.reportPDF)
}

func pdfFilename(name string) string {
	safe := strings.Map(func(r rune) rune {
		if r == ' ' {
			return '_'
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return -1
	}, name)
	return safe + ".pdf"
}

func (s *Server) patientPDF(w http.ResponseWriter, r *http.Request) {
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
	jobs, _, err := s.store.ListJobs(r.Context(), db.JobFilter{Search: p.MemberID, Limit: 100})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	own := jobs[:0:0]
	for _, j := range jobs {
		if j.PatientID == p.ID {
			own = append(own, j)
		}
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, pdfFilename(p.Name+"_record")))
	if err := pdfreport.PatientPDF(w, p, own); err != nil {
		s.log.Error("patient pdf render failed", "err", err)
	}
}

func (s *Server) reportPDF(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.Stats(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="verafy_report.pdf"`)
	if err := pdfreport.ReportPDF(w, st); err != nil {
		s.log.Error("report pdf render failed", "err", err)
	}
}
