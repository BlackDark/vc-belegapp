package storage

import (
	"context"
	"errors"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/minio-go/v7/pkg/encrypt"
)

// S3Options configures the S3 backend.
type S3Options struct {
	Endpoint        string
	Region          string
	Bucket          string
	Prefix          string
	AccessKeyID     string
	SecretAccessKey string
	UseTLS          bool
	ForcePathStyle  bool
	SSE             bool
}

// S3 is a BlobStore on any S3-compatible endpoint.
type S3 struct {
	client  *minio.Client
	bucket  string
	prefix  string
	sse     encrypt.ServerSide
	mu      sync.Mutex
	pingAt  time.Time
	pingErr error
}

// NewS3 builds a client. Ping checks the bucket and caches that result for 30s.
func NewS3(opts S3Options) (*S3, error) {
	if opts.Bucket == "" {
		return nil, errors.New("storage: s3 bucket is required")
	}
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		return nil, errors.New("storage: s3 endpoint is required")
	}
	if strings.Contains(endpoint, "://") {
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" {
			return nil, errors.New("storage: s3 endpoint is invalid")
		}
		endpoint = u.Host
	}
	creds := credentials.NewStaticV4(opts.AccessKeyID, opts.SecretAccessKey, "")
	lookup := minio.BucketLookupAuto
	if opts.ForcePathStyle {
		lookup = minio.BucketLookupPath
	}
	client, err := minio.New(endpoint, &minio.Options{
		Creds:        creds,
		Secure:       opts.UseTLS,
		Region:       opts.Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, err
	}
	var sse encrypt.ServerSide
	if opts.SSE {
		sse = encrypt.NewSSE()
	}
	prefix := opts.Prefix
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return &S3{client: client, bucket: opts.Bucket, prefix: prefix, sse: sse}, nil
}

func (s *S3) Ping(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.pingAt.IsZero() && time.Since(s.pingAt) < 30*time.Second {
		return s.pingErr
	}
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		s.pingAt = time.Now()
		s.pingErr = err
		return err
	}
	if !exists {
		err = errors.New("storage: s3 bucket does not exist")
	}
	s.pingAt = time.Now()
	s.pingErr = err
	return err
}

func (s *S3) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, s.prefix+key, r, size, minio.PutObjectOptions{
		ContentType:          contentType,
		ServerSideEncryption: s.sse,
	})
	return err
}

func (s *S3) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, s.prefix+key, minio.GetObjectOptions{})
	if err != nil {
		return nil, ObjectInfo{}, mapNotFound(err)
	}
	st, err := obj.Stat()
	if err != nil {
		_ = obj.Close()
		return nil, ObjectInfo{}, mapNotFound(err)
	}
	return obj, ObjectInfo{Key: key, Size: st.Size, ContentType: st.ContentType, ModTime: st.LastModified}, nil
}

func (s *S3) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := ValidateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	st, err := s.client.StatObject(ctx, s.bucket, s.prefix+key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, mapNotFound(err)
	}
	return ObjectInfo{Key: key, Size: st.Size, ContentType: st.ContentType, ModTime: st.LastModified}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	err := s.client.RemoveObject(ctx, s.bucket, s.prefix+key, minio.RemoveObjectOptions{})
	if err == nil || isNotFound(err) {
		return nil
	}
	return err
}

func (s *S3) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	if prefix != "" {
		trimmed := strings.Trim(prefix, "/")
		if trimmed != "" {
			if err := ValidateKey(trimmed); err != nil {
				return err
			}
		}
	}
	opts := minio.ListObjectsOptions{Prefix: s.prefix + prefix, Recursive: true}
	for obj := range s.client.ListObjects(ctx, s.bucket, opts) {
		if obj.Err != nil {
			return obj.Err
		}
		key := strings.TrimPrefix(obj.Key, s.prefix)
		if err := fn(ObjectInfo{Key: key, Size: obj.Size, ContentType: obj.ContentType, ModTime: obj.LastModified}); err != nil {
			return err
		}
	}
	return nil
}

func mapNotFound(err error) error {
	if isNotFound(err) {
		return ErrNotFound
	}
	return err
}

func isNotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.Code == "NotFound" || resp.StatusCode == 404
}
