package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
)

// ErrS3NotImplemented is returned until the S3 backend is implemented.
var ErrS3NotImplemented = errors.New("storage: s3 backend is not implemented")

// S3Options configures the S3 backend. The client is not constructed in M0.
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

// S3 is the S3 BlobStore. Object operations and Ping are not implemented yet.
type S3 struct {
	opts S3Options
}

// NewS3 stores options for a later implementation. Bucket must be set.
func NewS3(opts S3Options) (*S3, error) {
	if opts.Bucket == "" {
		return nil, errors.New("storage: s3 bucket is required")
	}
	return &S3{opts: opts}, nil
}

func (s *S3) notImplemented() error {
	return fmt.Errorf("%w (bucket %s)", ErrS3NotImplemented, s.opts.Bucket)
}

func (s *S3) Ping(context.Context) error { return s.notImplemented() }

func (s *S3) Put(context.Context, string, io.Reader, int64, string) error {
	return s.notImplemented()
}

func (s *S3) Get(context.Context, string) (io.ReadCloser, ObjectInfo, error) {
	return nil, ObjectInfo{}, s.notImplemented()
}

func (s *S3) Stat(context.Context, string) (ObjectInfo, error) {
	return ObjectInfo{}, s.notImplemented()
}

func (s *S3) Delete(context.Context, string) error { return s.notImplemented() }

func (s *S3) List(context.Context, string, func(ObjectInfo) error) error {
	return s.notImplemented()
}
