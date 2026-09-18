// Package ratelimit provides per-payer outbound token buckets whose state is
// observable, so the UI can show "throttled to respect X's rate limit" honestly.
package ratelimit

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"
)

type PayerLimiter struct {
	rps   float64
	burst int
	mu    sync.Mutex
	lims  map[string]*entry
}

type entry struct {
	lim       *rate.Limiter
	waiting   atomic.Int64
	inFlight  atomic.Int64
	throttled atomic.Int64 // total waits that actually blocked
	lastWait  atomic.Int64 // unix nanos of last blocking wait
}

type State struct {
	Payer     string  `json:"payer"`
	RPS       float64 `json:"rps"`
	Burst     int     `json:"burst"`
	Waiting   int64   `json:"waiting"`
	InFlight  int64   `json:"inFlight"`
	Throttled int64   `json:"throttledTotal"`
	Active    bool    `json:"throttlingNow"`
}

func New(rps float64, burst int) *PayerLimiter {
	return &PayerLimiter{rps: rps, burst: burst, lims: map[string]*entry{}}
}

func (p *PayerLimiter) get(payer string) *entry {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.lims[payer]
	if !ok {
		e = &entry{lim: rate.NewLimiter(rate.Limit(p.rps), p.burst)}
		p.lims[payer] = e
	}
	return e
}

// Acquire blocks until a token for the payer is available. Returns a release func.
func (p *PayerLimiter) Acquire(ctx context.Context, payer string) (func(), error) {
	e := p.get(payer)
	e.waiting.Add(1)
	start := time.Now()
	err := e.lim.Wait(ctx)
	e.waiting.Add(-1)
	if err != nil {
		return nil, err
	}
	if time.Since(start) > 20*time.Millisecond {
		e.throttled.Add(1)
		e.lastWait.Store(time.Now().UnixNano())
	}
	e.inFlight.Add(1)
	return func() { e.inFlight.Add(-1) }, nil
}

func (p *PayerLimiter) Snapshot() []State {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]State, 0, len(p.lims))
	now := time.Now().UnixNano()
	for k, e := range p.lims {
		out = append(out, State{
			Payer: k, RPS: p.rps, Burst: p.burst,
			Waiting: e.waiting.Load(), InFlight: e.inFlight.Load(), Throttled: e.throttled.Load(),
			Active: e.waiting.Load() > 0 || now-e.lastWait.Load() < int64(2*time.Second),
		})
	}
	return out
}
