package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestBlobStoreContract(t *testing.T) {
	t.Run("fs", func(t *testing.T) {
		dir := t.TempDir()
		store := NewFS(dir + "/blobs")
		exercise(t, store)
	})
	t.Run("s3", func(t *testing.T) {
		exercise(t, newFakeS3(t))
	})
}

func exercise(t *testing.T, store BlobStore) {
	t.Helper()
	ctx := context.Background()
	if err := store.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "../x", bytes.NewReader(nil), 0, ""); err == nil {
		t.Fatal("invalid key accepted")
	}
	body := []byte("hello-beleg")
	key := "bilder/ab/hello.txt"
	if err := store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), "text/plain"); err != nil {
		t.Fatal(err)
	}
	rc, info, err := store.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, body) || info.Size != int64(len(body)) || info.Key != key {
		t.Fatalf("get %#v %q", info, got)
	}
	if info.ContentType != "text/plain" {
		t.Fatalf("content type %q", info.ContentType)
	}
	st, err := store.Stat(ctx, key)
	if err != nil || st.Size != info.Size {
		t.Fatalf("stat %+v %v", st, err)
	}
	var listed []ObjectInfo
	if err := store.List(ctx, "bilder/", func(info ObjectInfo) error {
		listed = append(listed, info)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Key != key {
		t.Fatalf("list %+v", listed)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing %v", err)
	}
	if err := store.Put(ctx, key, bytes.NewReader(body), int64(len(body)+1), "text/plain"); err == nil {
		t.Fatal("size mismatch accepted")
	}
}

func newFakeS3(t *testing.T) *S3 {
	t.Helper()
	backend := s3mem.New()
	ts := httptest.NewServer(gofakes3.New(backend).Server())
	t.Cleanup(ts.Close)
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewS3(S3Options{
		Endpoint:        u.Host,
		Region:          "us-east-1",
		Bucket:          "belegapp",
		Prefix:          "belegapp/",
		AccessKeyID:     "test",
		SecretAccessKey: "testtest",
		UseTLS:          false,
		ForcePathStyle:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	client, err := minio.New(u.Host, &minio.Options{
		Creds:        credentials.NewStaticV4("test", "testtest", ""),
		Secure:       false,
		Region:       "us-east-1",
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.MakeBucket(context.Background(), "belegapp", minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(err)
	}
	return store
}

func TestS3RequiresBucket(t *testing.T) {
	if _, err := NewS3(S3Options{}); err == nil {
		t.Fatal("expected bucket error")
	}
	if _, err := NewS3(S3Options{Bucket: "belegapp"}); err == nil {
		t.Fatal("expected endpoint error")
	}
}
