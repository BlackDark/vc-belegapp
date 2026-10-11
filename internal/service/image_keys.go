package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/BlackDark/vc-belegapp/internal/db"
	"github.com/BlackDark/vc-belegapp/internal/storage"
)

// BildBlobKey is the content-addressed JPEG key from SPEC §11.1.
func BildBlobKey(sha string) (string, bool) {
	if !sha256Hex(sha) {
		return "", false
	}
	return "bilder/" + sha[:2] + "/" + sha + ".jpg", true
}

// ThumbBlobKey is the thumbnail key paired with the same stored-file hash.
func ThumbBlobKey(sha string) (string, bool) {
	if !sha256Hex(sha) {
		return "", false
	}
	return "thumbs/" + sha[:2] + "/" + sha + ".jpg", true
}

func sha256Hex(sha string) bool {
	if len(sha) != 64 {
		return false
	}
	for _, r := range sha {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// BildKeyMigration is the result of one startup or post-import pass.
// Skipped rows keep their current keys (missing blob or hash mismatch).
type BildKeyMigration struct {
	Moved   int
	Skipped []string
}

// MigrateBildKeys copies per-id blobs onto content-addressed keys.
// It is safe to run again: rows that already use the target keys are left
// alone, and a destination object with the same bytes is not rewritten.
// The previous object is deleted only when no Belegbild and no Monatsexport
// still names it. Unreferenced leftovers of the old layout are removed in
// the same pass. Content-addressed orphans stay for the unassigned TTL.
func (s *Service) MigrateBildKeys(ctx context.Context) (BildKeyMigration, error) {
	var out BildKeyMigration
	if s == nil || s.Store == nil || s.DB == nil {
		return out, nil
	}
	q := db.New(s.DB.Write)
	rows, err := q.ListBelegbilder(ctx)
	if err != nil {
		return out, err
	}
	for _, row := range rows {
		blobKey, ok := BildBlobKey(row.Sha256)
		thumbKey, okThumb := ThumbBlobKey(row.Sha256)
		if !ok || !okThumb {
			out.Skipped = append(out.Skipped, row.ID)
			continue
		}
		if row.BlobKey == blobKey && row.ThumbBlobKey == thumbKey {
			continue
		}
		skipped, err := s.relocateBild(ctx, q, row, blobKey, thumbKey)
		if err != nil {
			return out, err
		}
		if skipped {
			out.Skipped = append(out.Skipped, row.ID)
			continue
		}
		out.Moved++
	}
	if err := s.dropLegacyBildBlobs(ctx); err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) relocateBild(ctx context.Context, q *db.Queries, row db.Belegbilder, blobKey, thumbKey string) (bool, error) {
	var image []byte
	if row.BlobKey != blobKey {
		body, err := readBlob(ctx, s.Store, row.BlobKey)
		if errors.Is(err, storage.ErrNotFound) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		sum, err := imagingHash(body)
		if err != nil {
			return false, err
		}
		if sum != row.Sha256 || int64(len(body)) != row.Bytes {
			return true, nil
		}
		image = body
	}
	var thumb []byte
	if row.ThumbBlobKey != thumbKey {
		body, err := readBlob(ctx, s.Store, row.ThumbBlobKey)
		if errors.Is(err, storage.ErrNotFound) {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		thumb = body
	}
	if image != nil {
		if err := s.putBytesIfChanged(ctx, blobKey, image); err != nil {
			return false, err
		}
	}
	if thumb != nil {
		if err := s.putBytesIfChanged(ctx, thumbKey, thumb); err != nil {
			return false, err
		}
	}
	if err := q.UpdateBelegbildKeys(ctx, db.UpdateBelegbildKeysParams{
		BlobKey:      blobKey,
		ThumbBlobKey: thumbKey,
		ID:           row.ID,
	}); err != nil {
		return false, err
	}
	if row.BlobKey != blobKey {
		s.deleteBlobIfFree(ctx, q, row.BlobKey)
	}
	if row.ThumbBlobKey != thumbKey {
		s.deleteBlobIfFree(ctx, q, row.ThumbBlobKey)
	}
	return false, nil
}

func (s *Service) putBytesIfChanged(ctx context.Context, key string, body []byte) error {
	info, err := s.Store.Stat(ctx, key)
	if err == nil && info.Size == int64(len(body)) {
		rc, _, gerr := s.Store.Get(ctx, key)
		if gerr == nil {
			existing, rerr := io.ReadAll(rc)
			_ = rc.Close()
			if rerr == nil && bytes.Equal(existing, body) {
				return nil
			}
		}
	} else if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	return s.Store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "image/jpeg")
}

// dropLegacyBildBlobs removes unreferenced per-id objects left by a crash
// between the copy and the row update. Referenced keys stay.
func (s *Service) dropLegacyBildBlobs(ctx context.Context) error {
	refs, err := s.referencedBlobKeys(ctx)
	if err != nil {
		return err
	}
	return s.Store.List(ctx, "bilder/", func(info storage.ObjectInfo) error {
		if !legacyBildKey(info.Key) {
			return nil
		}
		if _, ok := refs[info.Key]; ok {
			return nil
		}
		return s.Store.Delete(ctx, info.Key)
	})
}

func legacyBildKey(key string) bool {
	name, ok := strings.CutPrefix(key, "bilder/")
	if !ok || strings.Contains(name, "/") || !strings.HasSuffix(name, ".jpg") {
		return false
	}
	stem := strings.TrimSuffix(name, ".jpg")
	stem = strings.TrimSuffix(stem, ".thumb")
	if len(stem) != 26 {
		return false
	}
	for _, r := range stem {
		if (r < '0' || r > '9') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}
