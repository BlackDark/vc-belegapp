package storage

import (
	"context"
	"io"
	"os"
)

// FS is a filesystem blob store rooted at Dir.
// Only Ping is implemented; object operations return ErrNotImplemented.
type FS struct {
	Dir string
}

// NewFS returns a filesystem store. Dir is created by Ping.
func NewFS(dir string) *FS {
	return &FS{Dir: dir}
}

// Ping creates Dir (mode 0750) and checks that it is writable.
func (s *FS) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(s.Dir, ".ping-*")
	if err != nil {
		return err
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func (s *FS) Put(context.Context, string, io.Reader, int64, string) error {
	return ErrNotImplemented
}

func (s *FS) Get(context.Context, string) (io.ReadCloser, ObjectInfo, error) {
	return nil, ObjectInfo{}, ErrNotImplemented
}

func (s *FS) Stat(context.Context, string) (ObjectInfo, error) {
	return ObjectInfo{}, ErrNotImplemented
}

func (s *FS) Delete(context.Context, string) error {
	return ErrNotImplemented
}

func (s *FS) List(context.Context, string, func(ObjectInfo) error) error {
	return ErrNotImplemented
}
