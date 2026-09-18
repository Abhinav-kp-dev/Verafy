package queue

import (
	"context"
	"errors"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// QueueStatus reports whether the worker pool is actively pulling new jobs.
type QueueStatus struct {
	Paused   bool       `json:"paused"`
	PausedAt *time.Time `json:"pausedAt,omitempty"`
}

// Status reports whether the default queue is currently paused. A queue that
// has never been paused/resumed returns ErrNotFound from River — that's a
// normal "never touched, so not paused" state, not an error.
func (q *Queue) Status(ctx context.Context) (QueueStatus, error) {
	info, err := q.Client.QueueGet(ctx, river.QueueDefault)
	if errors.Is(err, river.ErrNotFound) {
		return QueueStatus{Paused: false}, nil
	}
	if err != nil {
		return QueueStatus{}, err
	}
	return QueueStatus{Paused: info.PausedAt != nil, PausedAt: info.PausedAt}, nil
}

// Pause stops the worker pool from pulling any new jobs. Jobs already in
// flight finish normally; nothing already queued is lost — they resume
// exactly where they left off when Resume is called.
func (q *Queue) Pause(ctx context.Context) error {
	return q.Client.QueuePause(ctx, river.QueueDefault, &river.QueuePauseOpts{})
}

func (q *Queue) Resume(ctx context.Context) error {
	return q.Client.QueueResume(ctx, river.QueueDefault, &river.QueuePauseOpts{})
}

// riverDeleteManyMaxCount is River's own hard cap on JobDeleteManyParams.First —
// it panics above this, so PurgeQueued must page rather than ask for everything
// in one call.
const riverDeleteManyMaxCount = 10000

// PurgeQueued permanently deletes jobs that are still waiting to run (never
// started). Used to clear a stale/oversized batch (e.g. a leftover synthetic
// load test) instead of letting it keep burning through real payer API calls.
// In-flight (running) and already-completed jobs are untouched.
func (q *Queue) PurgeQueued(ctx context.Context) (int, error) {
	total := 0
	for {
		res, err := q.Client.JobDeleteMany(ctx, river.NewJobDeleteManyParams().
			Queues(river.QueueDefault).
			States(rivertype.JobStateAvailable, rivertype.JobStateScheduled, rivertype.JobStateRetryable).
			First(riverDeleteManyMaxCount))
		if err != nil {
			return total, err
		}
		total += len(res.Jobs)
		if len(res.Jobs) < riverDeleteManyMaxCount {
			return total, nil
		}
	}
}
