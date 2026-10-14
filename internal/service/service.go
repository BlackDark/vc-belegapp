// Package service implements receipt capture, year rules, and the audit trail.
package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/mailinbox"
	"github.com/BlackDark/vc-belegapp/internal/notify"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
	"github.com/BlackDark/vc-belegapp/internal/recognition"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// Actor is the signed-in user and the request id written to the audit log.
type Actor struct {
	Name      string
	RequestID string
}

// Service is the application API used by HTTP handlers.
type Service struct {
	DB             *db.DB
	Store          storage.BlobStore
	Holidays       holidays.Provider
	Loc            *time.Location
	Now            func() time.Time
	UploadMax      int64
	ImageTTL       time.Duration
	RetentionYears int
	Extractor      recognition.ReceiptExtractor
	Jobs           *jobs.Queue
	LLMMaxPX       int
	LLMTimeout     time.Duration
	PDF            pdf.Renderer
	AppVersion     string
	Log            *slog.Logger

	// Mail is the IMAP intake. It is nil when no mailbox is configured, and
	// IngestMailbox is a no-op in that case. MailInterval is the poll period,
	// MailMaxMessages how many messages one poll takes, MailMaxAttachments the
	// per-message picture cap, and MailDoneFolder where an ingested message is
	// moved (empty: only flag it).
	Mail               mailinbox.Source
	MailInterval       time.Duration
	MailMaxMessages    int
	MailMaxAttachments int
	MailDoneFolder     string
	MailAllowedSenders []string

	// Notify posts the bild.pruefen webhook when an image lands unassigned. It
	// is nil unless the operator set BELEGAPP_NOTIFY_WEBHOOK_URL, and
	// enqueueBildPruefen returns on that check, so an instance without the
	// feature never builds a payload or writes a queue row.
	Notify *notify.Webhook

	// Watch is the watched-folder intake. WatchFolder is the switch: empty
	// means PollWatchFolder returns at once and IngestWatchFolder is a no-op,
	// so an instance that does not use the feature never grows a queue row.
	// WatchInterval is the poll period. watchSeen is the in-memory stability
	// memory and is not operator configuration.
	WatchFolder   string
	WatchInterval time.Duration
	watch         watchSeen

	exportMu  sync.Mutex
	exporting map[string]struct{}

	restoreMu sync.Mutex
	importing atomic.Bool
	importMu  sync.Mutex
	imports   map[string]importTicket

	jobParent context.Context
	jobCancel context.CancelFunc
	jobWG     sync.WaitGroup
	// jobStopTimeout overrides jobShutdownTimeout for the stop of the workers.
	// Zero is the normal case and means the constant; only the test sets it.
	jobStopTimeout time.Duration
	// sweepBatchRows overrides defaultSweepBatch, the number of expired images
	// one pass of the image sweeper reads at a time. Zero is the normal case and
	// means the constant; only the test sets it, to make a multi-batch pass
	// cheap enough to run on a small fixture.
	sweepBatchRows int
}

// ImportLaeuft reports whether a data import currently holds the write lock.
func (s *Service) ImportLaeuft() bool {
	return s != nil && s.importing.Load()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) today() string {
	loc := s.Loc
	if loc == nil {
		loc = time.UTC
	}
	return s.now().In(loc).Format("2006-01-02")
}

func (s *Service) stamp() string {
	return s.now().UTC().Format(time.RFC3339)
}

func (s *Service) tx(ctx context.Context, fn func(context.Context, *db.Queries, *sql.Tx) error) error {
	tx, err := s.DB.Write.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(ctx, db.New(tx), tx); err != nil {
		return err
	}
	return tx.Commit()
}

func isNoRows(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
