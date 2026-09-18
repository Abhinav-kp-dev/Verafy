// Package events bridges Postgres LISTEN/NOTIFY (fired by the jobs trigger) to
// Server-Sent Events. Job changes are coalesced every 250ms; batch counters are
// re-read for any batch touched in that window and pushed as a single event.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"coveragecheck/internal/db"
	"coveragecheck/internal/ratelimit"
)

type jobChange struct {
	JobID        uuid.UUID  `json:"job_id"`
	BatchID      *uuid.UUID `json:"batch_id"`
	Status       string     `json:"status"`
	AttemptCount int        `json:"attempt_count"`
}

type Hub struct {
	store   *db.Store
	limiter *ratelimit.PayerLimiter
	log     *slog.Logger

	mu      sync.Mutex
	clients map[chan []byte]struct{}

	pending   []jobChange
	pendingMu sync.Mutex
}

func New(store *db.Store, limiter *ratelimit.PayerLimiter, log *slog.Logger) *Hub {
	return &Hub{store: store, limiter: limiter, log: log, clients: map[chan []byte]struct{}{}}
}

// Run listens on the job_changes channel and flushes coalesced events. Blocks until ctx is done.
func (h *Hub) Run(ctx context.Context, pool *pgxpool.Pool) {
	go h.flushLoop(ctx)
	for ctx.Err() == nil {
		if err := h.listen(ctx, pool); err != nil && ctx.Err() == nil {
			h.log.Error("listen error; reconnecting", "err", err)
			time.Sleep(time.Second)
		}
	}
}

func (h *Hub) listen(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN job_changes"); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		var jc jobChange
		if err := json.Unmarshal([]byte(n.Payload), &jc); err != nil {
			continue
		}
		h.pendingMu.Lock()
		h.pending = append(h.pending, jc)
		h.pendingMu.Unlock()
	}
}

func (h *Hub) flushLoop(ctx context.Context) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	statsTick := time.NewTicker(2 * time.Second)
	defer statsTick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.flush(ctx)
		case <-statsTick.C:
			if h.clientCount() > 0 {
				h.broadcast("ratelimit", h.limiter.Snapshot())
			}
		}
	}
}

func (h *Hub) flush(ctx context.Context) {
	h.pendingMu.Lock()
	batch := h.pending
	h.pending = nil
	h.pendingMu.Unlock()
	if len(batch) == 0 || h.clientCount() == 0 {
		return
	}
	h.broadcast("jobs", batch)

	seen := map[uuid.UUID]bool{}
	ids := []uuid.UUID{}
	for _, jc := range batch {
		if jc.BatchID != nil && !seen[*jc.BatchID] {
			seen[*jc.BatchID] = true
			ids = append(ids, *jc.BatchID)
		}
	}
	if len(ids) > 0 {
		if batches, err := h.store.GetBatches(ctx, ids); err == nil {
			h.broadcast("batches", batches)
		}
	}
}

func (h *Hub) clientCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func (h *Hub) broadcast(event string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	msg := []byte("event: " + event + "\ndata: " + string(data) + "\n\n")
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // slow client: drop rather than block the hub
		}
	}
}

// ServeHTTP is the SSE endpoint.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
	}()

	w.Write([]byte("event: hello\ndata: {\"ok\":true}\n\n"))
	flusher.Flush()
	keep := time.NewTicker(15 * time.Second)
	defer keep.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			w.Write(msg)
			flusher.Flush()
		case <-keep.C:
			w.Write([]byte(": keepalive\n\n"))
			flusher.Flush()
		}
	}
}
