package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// A count that cannot answer must not be read as "the blob is free", and it
// must not be silent either: a lookup that always fails would otherwise leave
// every blob of every deleted image on disk with no line saying so.
func TestDeleteBlobIfFreeReportsAFailedCount(t *testing.T) {
	ctx := context.Background()
	var buf syncBuffer
	svc := openService(t, nil)
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))

	key := "bilder/abc.jpg"
	if err := svc.Store.Put(ctx, key, strings.NewReader("x"), 1, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	// Renaming the table the first count reads makes exactly that count fail.
	// The blob is referenced by nothing, but "unknown" must not be treated as
	// "free".
	if _, err := svc.DB.WriteDB().ExecContext(ctx, `ALTER TABLE belegbilder RENAME TO belegbilder_weg`); err != nil {
		t.Fatal(err)
	}

	svc.deleteBlobIfFree(ctx, db.New(svc.DB.WriteDB()), key)

	if _, err := svc.Store.Stat(ctx, key); err != nil {
		t.Fatal("an unanswered count deleted the blob")
	}
	assertLogged(t, buf.String(), "blob reference count failed", key)
}

// The delete is the last step, and a delete that fails leaves an unreferenced
// blob on disk forever. It must not fail the caller, but it has to be visible.
func TestDeleteBlobIfFreeReportsAFailedDelete(t *testing.T) {
	ctx := context.Background()
	var buf syncBuffer
	svc := openService(t, nil)
	svc.Log = slog.New(slog.NewJSONHandler(&buf, nil))

	key := "bilder/def.jpg"
	if err := svc.Store.Put(ctx, key, strings.NewReader("x"), 1, "image/jpeg"); err != nil {
		t.Fatal(err)
	}
	svc.Store = deleteFailStore{BlobStore: svc.Store, err: errors.New("delete refused")}

	svc.deleteBlobIfFree(ctx, db.New(svc.DB.WriteDB()), key)

	if _, err := svc.Store.Stat(ctx, key); err != nil {
		t.Fatal("the store lost the blob while its delete was refused")
	}
	assertLogged(t, buf.String(), "unreferenced blob could not be deleted", key)
}

// deleteFailStore refuses exactly the delete, so a test can hold the other
// steps to their behaviour without a store that is broken in some other way.
type deleteFailStore struct {
	storage.BlobStore
	err error
}

func (s deleteFailStore) Delete(context.Context, string) error { return s.err }
