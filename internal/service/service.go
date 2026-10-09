// Package service implements receipt capture, year rules, and the audit trail.
package service

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/erkennung"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/jobs"
	"github.com/BlackDark/vc-belegapp/internal/pdf"
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
	Extractor      erkennung.ReceiptExtractor
	Jobs           *jobs.Queue
	LLMMaxPX       int
	LLMTimeout     time.Duration
	PDF            pdf.Renderer
	AppVersion     string

	exportMu  sync.Mutex
	exporting map[string]struct{}

	restoreMu sync.Mutex
	importing atomic.Bool
	importMu  sync.Mutex
	imports   map[string]importTicket

	jobParent context.Context
	jobCancel context.CancelFunc
	jobWG     sync.WaitGroup
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
