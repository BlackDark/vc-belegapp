package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
)

// pollLoop queues one intake job at once and then one on every tick of every,
// until ctx ends.
//
// It is the shape both intake pollers had written out: the first tick is a
// separate enqueue rather than a first pass through the loop, because an
// instance that restarts should not wait out a whole interval before its first
// poll. The loop itself only ticks — the job queue owns the intake I/O and the
// retries — so nothing here can block a shutdown for longer than one tick.
//
// An intake without a source, without a queue, or without a positive interval
// returns before the first enqueue, which is what keeps an instance that did not
// configure the feature from growing a queue row.
func (s *Service) pollLoop(ctx context.Context, every time.Duration, enqueue func(context.Context)) {
	if s.Jobs == nil || every <= 0 {
		return
	}
	enqueue(ctx)
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			enqueue(ctx)
		}
	}
}

// enqueueUnlessOpen queues one job of typ unless a job with the same payload is
// still waiting or running.
//
// The skip is the whole point: an intake that cannot finish — an unreachable
// mail server, a folder on a stalled disk — already has a job in the queue, and
// its retry backoff is what decides when the next attempt happens. Without the
// count every tick would add another job on top of the retries already there,
// and the intake would turn one failure into a backlog that nothing drains.
//
// A count or an insert that fails is logged and otherwise ignored: the next tick
// asks again, so a single failed write costs one interval and nothing else.
func (s *Service) enqueueUnlessOpen(ctx context.Context, typ, payload string) {
	n, err := db.New(s.DB.Write).CountOpenJobsByPayload(ctx, db.CountOpenJobsByPayloadParams{
		Typ:     typ,
		Payload: payload,
	})
	if err != nil {
		s.log(typ+" job could not be counted", "err", err)
		return
	}
	if n > 0 {
		return
	}
	if _, err := s.Jobs.Enqueue(ctx, typ, payload); err != nil {
		s.log(typ+" job could not be enqueued", "err", err)
	}
}

// log writes one line about background work: an intake that ran, a queue write
// that failed, a sweep that could not finish.
//
// It is nil-safe because a Service without a logger is a supported shape — the
// test fixtures and a program that embeds the service build one — and the
// background goroutines must not need a special case for it.
func (s *Service) log(msg string, args ...any) {
	s.logAt(slog.LevelInfo, msg, args...)
}

// logWarn writes one line about a finding an operator should look at.
//
// The level is what separates it from log: a warning is something a person may
// have to act on, and an error rate that answers "is the intake healthy?" must
// not be built out of lines that were always going to be written.
func (s *Service) logWarn(msg string, args ...any) {
	s.logAt(slog.LevelWarn, msg, args...)
}

// logAt is the one place a background line is written.
//
// slog.Logger.Info and .Warn both take a context for handlers that want one; a
// background loop has none to hand over, so it gets a fresh one rather than a
// cancelled context that a handler might read as a shutdown.
func (s *Service) logAt(level slog.Level, msg string, args ...any) {
	if s.Log == nil {
		return
	}
	s.Log.Log(context.Background(), level, msg, args...)
}
