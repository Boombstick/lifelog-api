package photostore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"lifelog-api/internal/trips"
)

func newDisk(t *testing.T) (*Disk, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "uploads", "trip-photos")
	d, err := New(root)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return d, root
}

func readAll(t *testing.T, d *Disk, name string) []byte {
	t.Helper()
	rc, err := d.Open(context.Background(), name)
	if err != nil {
		t.Fatalf("Open(%q): %v", name, err)
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read %q: %v", name, err)
	}
	return b
}

func entries(t *testing.T, dir string) []string {
	t.Helper()
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	names := make([]string, 0, len(list))
	for _, e := range list {
		names = append(names, e.Name())
	}
	return names
}

func TestNewCreatesRootAndResolvesRelative(t *testing.T) {
	d, root := newDisk(t)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("root not created: %v", err)
	}
	if d.root != root {
		t.Fatalf("root = %q, want %q", d.root, root)
	}

	t.Chdir(t.TempDir())
	rel, err := New("uploads/trip-photos")
	if err != nil {
		t.Fatalf("New relative: %v", err)
	}
	if !filepath.IsAbs(rel.root) {
		t.Fatalf("relative root kept relative: %q", rel.root)
	}
}

func TestSaveThenOpen(t *testing.T) {
	d, root := newDisk(t)
	body := bytes.Repeat([]byte{0xFF, 0xD8, 0x00}, 50_000)
	name := "0f8fad5bd9cb469fa16570867728950e.jpg"

	if err := d.Save(context.Background(), name, bytes.NewReader(body)); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := readAll(t, d, name); !bytes.Equal(got, body) {
		t.Fatalf("content differs: %d bytes, want %d", len(got), len(body))
	}
	if got := entries(t, root); len(got) != 1 || got[0] != name {
		t.Fatalf("root holds %v, want only %s flat in the root", got, name)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o044 != 0o044 {
			t.Fatalf("file mode %v is not group/world readable", perm)
		}
	}
}

func TestOpenReadsFileLaidOutByCSharp(t *testing.T) {
	d, root := newDisk(t)

	name := "edd925b54fa748f2a8dcefe1c59975fc.png"
	want := []byte("\x89PNG\r\n\x1a\nfrom C#")
	if err := os.WriteFile(filepath.Join(root, name), want, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, d, name); !bytes.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if err := d.Delete(context.Background(), name); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, name)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file still there: %v", err)
	}
}

func TestSaveRefusesExistingName(t *testing.T) {
	d, root := newDisk(t)
	name := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.webp"
	if err := os.WriteFile(filepath.Join(root, name), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := d.Save(context.Background(), name, strings.NewReader("replacement"))
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Save over existing = %v, want fs.ErrExist", err)
	}
	if got := readAll(t, d, name); string(got) != "original" {
		t.Fatalf("existing file changed to %q", got)
	}
}

type failingReader struct{ sent bool }

func (r *failingReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "partial"), nil
	}
	return 0, errors.New("connection reset")
}

func TestSaveFailureLeavesNothing(t *testing.T) {
	d, root := newDisk(t)
	name := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.jpg"

	if err := d.Save(context.Background(), name, &failingReader{}); err == nil {
		t.Fatal("Save with a failing body succeeded")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Save(ctx, name, strings.NewReader("data")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save with a cancelled context = %v, want context.Canceled", err)
	}
	if got := entries(t, root); len(got) != 0 {
		t.Fatalf("root holds %v after failed saves, want nothing", got)
	}
	if _, err := d.Open(context.Background(), name); !errors.Is(err, trips.ErrPhotoFileMissing) {
		t.Fatalf("Open after failed save = %v, want ErrPhotoFileMissing", err)
	}
}

func TestOpenMissing(t *testing.T) {
	d, root := newDisk(t)
	if _, err := d.Open(context.Background(), "cccccccccccccccccccccccccccccccc.heic"); !errors.Is(err, trips.ErrPhotoFileMissing) {
		t.Fatalf("Open missing = %v, want ErrPhotoFileMissing", err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir.jpg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Open(context.Background(), "dir.jpg"); !errors.Is(err, trips.ErrPhotoFileMissing) {
		t.Fatalf("Open directory = %v, want ErrPhotoFileMissing", err)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	d, root := newDisk(t)
	name := "dddddddddddddddddddddddddddddddd.jpg"
	if err := d.Save(context.Background(), name, strings.NewReader("x")); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if err := d.Delete(context.Background(), name); err != nil {
			t.Fatalf("Delete #%d: %v", i+1, err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "keep.jpg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := d.Delete(context.Background(), "keep.jpg"); err != nil {
		t.Fatalf("Delete directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "keep.jpg")); err != nil {
		t.Fatalf("directory under a photo name was removed: %v", err)
	}
}

func TestTraversalIsRejectedOrContained(t *testing.T) {
	d, root := newDisk(t)
	ctx := context.Background()

	parent := filepath.Dir(root)
	secrets := []string{filepath.Join(parent, "secret.jpg"), filepath.Join(filepath.Dir(parent), "secret.jpg")}
	for _, p := range secrets {
		if err := os.WriteFile(p, []byte("secret"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	unsafe := []string{"", ".", "..", "/", "../..", ".upload-123"}
	if runtime.GOOS == "windows" {
		unsafe = append(unsafe, `..\..`, `\`)
	}
	for _, name := range unsafe {
		if _, err := d.Open(ctx, name); !errors.Is(err, trips.ErrUnsafePhotoPath) {
			t.Errorf("Open(%q) = %v, want ErrUnsafePhotoPath", name, err)
		}
		if err := d.Delete(ctx, name); !errors.Is(err, trips.ErrUnsafePhotoPath) {
			t.Errorf("Delete(%q) = %v, want ErrUnsafePhotoPath", name, err)
		}
		if err := d.Save(ctx, name, strings.NewReader("x")); !errors.Is(err, trips.ErrUnsafePhotoPath) {
			t.Errorf("Save(%q) = %v, want ErrUnsafePhotoPath", name, err)
		}
	}

	contained := []string{"../secret.jpg", "../../secret.jpg", "/etc/../secret.jpg", secrets[0]}
	if runtime.GOOS == "windows" {
		contained = append(contained, `..\secret.jpg`, `C:\secret.jpg`)
	}
	for _, name := range contained {
		if _, err := d.Open(ctx, name); !errors.Is(err, trips.ErrPhotoFileMissing) {
			t.Errorf("Open(%q) = %v, want ErrPhotoFileMissing", name, err)
		}
		if err := d.Delete(ctx, name); err != nil {
			t.Errorf("Delete(%q) = %v, want nil", name, err)
		}
	}
	if err := d.Save(ctx, "../escaped.jpg", strings.NewReader("inside")); err != nil {
		t.Fatalf("Save(../escaped.jpg): %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "escaped.jpg")); err != nil {
		t.Fatalf("stripped name not saved inside the root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, "escaped.jpg")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file escaped the root: %v", err)
	}
	for _, p := range secrets {
		if b, err := os.ReadFile(p); err != nil || string(b) != "secret" {
			t.Fatalf("secret outside the root was touched: %v", err)
		}
	}
}
