package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// FS is a filesystem blob store rooted at Dir.
// Writes go to a temp file, are fsynced, then renamed. Directories are 0750, files 0640.
type FS struct {
	Dir string
}

// NewFS returns a filesystem store. Dir is created by the first operation.
func NewFS(dir string) *FS {
	return &FS{Dir: dir}
}

func (s *FS) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.withRoot(func(root *os.Root) error {
		if err := os.Chmod(s.Dir, 0o750); err != nil {
			return err
		}
		name, err := tempName()
		if err != nil {
			return err
		}
		f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			return err
		}
		if err := f.Close(); err != nil {
			_ = root.Remove(name)
			return err
		}
		return root.Remove(name)
	})
}

func (s *FS) Put(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateKey(key); err != nil {
		return err
	}
	return s.withRoot(func(root *os.Root) error {
		dir := path.Dir(key)
		if dir != "." {
			if err := root.MkdirAll(dir, 0o750); err != nil {
				return err
			}
		}
		tmp := path.Join(dir, mustTemp())
		f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
		if err != nil {
			return err
		}
		n, copyErr := io.Copy(f, r)
		syncErr := f.Sync()
		closeErr := f.Close()
		if copyErr != nil || syncErr != nil || closeErr != nil || (size >= 0 && n != size) {
			_ = root.Remove(tmp)
			if copyErr != nil {
				return copyErr
			}
			if size >= 0 && n != size {
				return errors.New("storage: size mismatch")
			}
			if syncErr != nil {
				return syncErr
			}
			return closeErr
		}
		if err := root.Chmod(tmp, 0o640); err != nil {
			_ = root.Remove(tmp)
			return err
		}
		if err := root.Rename(tmp, key); err != nil {
			_ = root.Remove(tmp)
			return err
		}
		return writeCtype(root, key, contentType)
	})
}

func (s *FS) Get(ctx context.Context, key string) (io.ReadCloser, ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, ObjectInfo{}, err
	}
	if err := ValidateKey(key); err != nil {
		return nil, ObjectInfo{}, err
	}
	root, err := s.open()
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	info, err := statKey(root, key)
	if err != nil {
		_ = root.Close()
		return nil, ObjectInfo{}, err
	}
	f, err := root.Open(key)
	if err != nil {
		_ = root.Close()
		if os.IsNotExist(err) {
			return nil, ObjectInfo{}, ErrNotFound
		}
		return nil, ObjectInfo{}, err
	}
	_ = root.Close()
	return f, info, nil
}

func (s *FS) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := ctx.Err(); err != nil {
		return ObjectInfo{}, err
	}
	if err := ValidateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	root, err := s.open()
	if err != nil {
		return ObjectInfo{}, err
	}
	defer func() { _ = root.Close() }()
	return statKey(root, key)
}

func (s *FS) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateKey(key); err != nil {
		return err
	}
	return s.withRoot(func(root *os.Root) error {
		err := root.Remove(key)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		err = root.Remove(key + ".ctype")
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}

func (s *FS) List(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if prefix != "" {
		if err := ValidateKey(strings.TrimSuffix(prefix, "/")); err != nil && prefix != "" {
			// A prefix may end with a slash, which ValidateKey allows only as part of a full key.
			trimmed := strings.Trim(prefix, "/")
			if trimmed != "" {
				if err := ValidateKey(trimmed); err != nil {
					return err
				}
			}
		}
	}
	root, err := s.open()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return walkRoot(root, prefix, fn)
}

func (s *FS) open() (*os.Root, error) {
	if err := os.MkdirAll(s.Dir, 0o750); err != nil {
		return nil, err
	}
	if err := os.Chmod(s.Dir, 0o750); err != nil {
		return nil, err
	}
	return os.OpenRoot(s.Dir)
}

func (s *FS) withRoot(fn func(*os.Root) error) error {
	root, err := s.open()
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	return fn(root)
}

func statKey(root *os.Root, key string) (ObjectInfo, error) {
	info, err := root.Stat(key)
	if err != nil {
		if os.IsNotExist(err) {
			return ObjectInfo{}, ErrNotFound
		}
		return ObjectInfo{}, err
	}
	ctype, _ := root.ReadFile(key + ".ctype")
	return ObjectInfo{
		Key:         key,
		Size:        info.Size(),
		ContentType: strings.TrimSpace(string(ctype)),
		ModTime:     info.ModTime(),
	}, nil
}

func writeCtype(root *os.Root, key, contentType string) error {
	name := key + ".ctype"
	if contentType == "" {
		_ = root.Remove(name)
		return nil
	}
	return root.WriteFile(name, []byte(contentType), 0o640)
}

func walkRoot(root *os.Root, prefix string, fn func(ObjectInfo) error) error {
	return walk(root, ".", prefix, fn)
}

func walk(root *os.Root, dir, prefix string, fn func(ObjectInfo) error) error {
	entries, err := readDir(root, dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		full := name
		if dir != "." {
			full = dir + "/" + name
		}
		if entry.IsDir() {
			if err := walk(root, full, prefix, fn); err != nil {
				return err
			}
			continue
		}
		if strings.HasSuffix(full, ".ctype") {
			continue
		}
		if prefix != "" && !strings.HasPrefix(full, prefix) {
			continue
		}
		info, err := statKey(root, full)
		if err != nil {
			return err
		}
		if err := fn(info); err != nil {
			return err
		}
	}
	return nil
}

func readDir(root *os.Root, dir string) ([]os.DirEntry, error) {
	f, err := root.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return f.ReadDir(-1)
}

func tempName() (string, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return ".tmp-" + hex.EncodeToString(buf[:]), nil
}

func mustTemp() string {
	name, err := tempName()
	if err != nil {
		return ".tmp-" + time.Now().UTC().Format("150405.000000000")
	}
	return name
}
