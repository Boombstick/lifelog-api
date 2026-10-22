package photostore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"lifelog-api/internal/trips"
)

type Disk struct {
	root string
}

func New(root string) (*Disk, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("photostore: resolve %q: %w", root, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("photostore: create root: %w", err)
	}
	return &Disk{root: abs}, nil
}

var _ trips.PhotoStore = (*Disk)(nil)

const tempPrefix = ".upload-"

func (d *Disk) Save(ctx context.Context, name string, body io.Reader) (err error) {
	path, err := d.path(name)
	if err != nil {
		return err
	}
	if _, statErr := os.Lstat(path); statErr == nil {
		return fmt.Errorf("photostore: %s: %w", filepath.Base(path), fs.ErrExist)
	}

	tmp, err := os.CreateTemp(d.root, tempPrefix+"*")
	if err != nil {
		return fmt.Errorf("photostore: create temp file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()

	if err = tmp.Chmod(0o644); err != nil {
		return fmt.Errorf("photostore: chmod: %w", err)
	}
	if _, err = io.Copy(tmp, ctxReader{ctx: ctx, r: body}); err != nil {
		return fmt.Errorf("photostore: write: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("photostore: sync: %w", err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("photostore: close: %w", err)
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("photostore: rename: %w", err)
	}
	return nil
}

func (d *Disk) Open(ctx context.Context, name string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := d.path(name)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, trips.ErrPhotoFileMissing
	}
	if err != nil {
		return nil, fmt.Errorf("photostore: open: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("photostore: stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, trips.ErrPhotoFileMissing
	}
	return f, nil
}

func (d *Disk) Delete(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := d.path(name)
	if err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("photostore: stat: %w", err)
	}
	if info.IsDir() {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("photostore: delete: %w", err)
	}
	return nil
}

func (d *Disk) path(name string) (string, error) {
	base := filepath.Base(name)
	if base == "." || base == ".." || base == string(filepath.Separator) ||
		strings.HasPrefix(base, tempPrefix) {
		return "", trips.ErrUnsafePhotoPath
	}
	path := filepath.Join(d.root, base)
	if filepath.Dir(path) != d.root {
		return "", trips.ErrUnsafePhotoPath
	}
	return path, nil
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
