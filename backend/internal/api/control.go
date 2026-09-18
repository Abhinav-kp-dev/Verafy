package api

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"coveragecheck/internal/db"
)

func (s *Server) registerControlRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/queue/status", s.queueStatus)
	mux.HandleFunc("POST /api/queue/pause", s.queuePause)
	mux.HandleFunc("POST /api/queue/resume", s.queueResume)
	mux.HandleFunc("POST /api/queue/purge", s.queuePurge)
}

func (s *Server) queueStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.queue.Status(r.Context())
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, st)
}

func (s *Server) queuePause(w http.ResponseWriter, r *http.Request) {
	if err := s.queue.Pause(r.Context()); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.log.Info("queue paused by staff action")
	st, _ := s.queue.Status(r.Context())
	writeJSON(w, 200, st)
}

func (s *Server) queueResume(w http.ResponseWriter, r *http.Request) {
	if err := s.queue.Resume(r.Context()); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.log.Info("queue resumed by staff action")
	st, _ := s.queue.Status(r.Context())
	writeJSON(w, 200, st)
}

// queuePurge deletes jobs still waiting to run (never started) — e.g. to clear
// a stale load test — without touching jobs already in flight or completed.
// It removes both sides consistently: the queued River task AND our own
// domain job row (cascading to its attempts/notices, adjusting batch
// counters), reusing the same delete path as the manual "delete" buttons in
// the UI, so a purged job never lingers as an orphaned QUEUED/RETRYING row.
func (s *Server) queuePurge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ids := s.pendingJobIDs(ctx)

	riverPurged, err := s.queue.PurgeQueued(ctx)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	domainDeleted, err := s.store.DeleteJobs(ctx, ids)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	s.log.Info("queued jobs purged by staff action", "riverTasks", riverPurged, "domainRows", domainDeleted)
	writeJSON(w, 200, map[string]int{"purged": domainDeleted})
}

// pendingJobIDs returns every domain job currently QUEUED or RETRYING — the
// ones a queue purge is about to remove the underlying River task for.
func (s *Server) pendingJobIDs(ctx context.Context) []uuid.UUID {
	ids, err := s.store.JobIDsByStatuses(ctx, db.StatusQueued, db.StatusRetrying)
	if err != nil {
		s.log.Error("pendingJobIDs", "err", err)
		return nil
	}
	return ids
}
