package service

import (
	"testing"
	"time"

	"github.com/BlackDark/vc-belegapp/internal/db/dbtest"
	"github.com/BlackDark/vc-belegapp/internal/holidays"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// newTestService builds a Service on the shared dbtest fixture: migrated
// database, timezone, and the fixed clock. A nil store becomes a fresh
// filesystem store in a temp directory. The suites that need more (a blob store
// of their own, a recognition extractor, an app version) set the remaining
// fields on the result.
func newTestService(t *testing.T, store storage.BlobStore) *Service {
	t.Helper()
	if store == nil {
		store = storage.NewFS(t.TempDir())
	}
	loc := dbtest.Loc(t)
	fixed := dbtest.Clock(loc)
	return &Service{
		DB:        dbtest.OpenSeeded(t),
		Store:     store,
		Holidays:  holidays.NewCalendar(),
		Loc:       loc,
		Now:       func() time.Time { return fixed },
		UploadMax: 1 << 20,
		ImageTTL:  time.Hour,
	}
}
