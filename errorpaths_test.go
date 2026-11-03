package switchfs

import (
	"errors"
	iofs "io/fs"
	"os"
	"testing"
	"time"

	"github.com/absfs/absfs"
)

// brokenFS fails every path method with an error naming the backend path it
// was given: a *os.PathError for one path and a *os.LinkError for two. It
// lets a test check that SwitchFS reports the caller's path for each method,
// which memfs alone can't (it returns a bare errno for several of them).
type brokenFS struct{ absfs.SymlinkFileSystem }

func (brokenFS) perr(op, name string) error {
	return &os.PathError{Op: op, Path: name, Err: errInjected}
}

func (b brokenFS) OpenFile(n string, _ int, _ os.FileMode) (absfs.File, error) {
	return nil, b.perr("open", n)
}
func (b brokenFS) Open(n string) (absfs.File, error)         { return nil, b.perr("open", n) }
func (b brokenFS) Create(n string) (absfs.File, error)       { return nil, b.perr("open", n) }
func (b brokenFS) Mkdir(n string, _ os.FileMode) error       { return b.perr("mkdir", n) }
func (b brokenFS) MkdirAll(n string, _ os.FileMode) error    { return b.perr("mkdir", n) }
func (b brokenFS) Remove(n string) error                     { return b.perr("remove", n) }
func (b brokenFS) RemoveAll(n string) error                  { return b.perr("RemoveAll", n) }
func (b brokenFS) Stat(n string) (os.FileInfo, error)        { return nil, b.perr("stat", n) }
func (b brokenFS) Lstat(n string) (os.FileInfo, error)       { return nil, b.perr("lstat", n) }
func (b brokenFS) Chmod(n string, _ os.FileMode) error       { return b.perr("chmod", n) }
func (b brokenFS) Chtimes(n string, _, _ time.Time) error    { return b.perr("chtimes", n) }
func (b brokenFS) Chown(n string, _, _ int) error            { return b.perr("chown", n) }
func (b brokenFS) Lchown(n string, _, _ int) error           { return b.perr("lchown", n) }
func (b brokenFS) Truncate(n string, _ int64) error          { return b.perr("truncate", n) }
func (b brokenFS) ReadDir(n string) ([]iofs.DirEntry, error) { return nil, b.perr("open", n) }
func (b brokenFS) ReadFile(n string) ([]byte, error)         { return nil, b.perr("open", n) }
func (b brokenFS) Readlink(n string) (string, error)         { return "", b.perr("readlink", n) }
func (b brokenFS) Rename(o, n string) error {
	return &os.LinkError{Op: "rename", Old: o, New: n, Err: errInjected}
}
func (b brokenFS) Symlink(o, n string) error {
	return &os.LinkError{Op: "symlink", Old: o, New: n, Err: errInjected}
}

// TestErrors_EveryMethodReportsVirtualPath checks, method by method, that a
// backend error naming the rewritten backend path comes back naming the
// caller's path, with the backend's Op and wrapped error kept.
func TestErrors_EveryMethodReportsVirtualPath(t *testing.T) {
	sfs, err := New(WithRoute("/v", brokenFS{mustMemFS(t)},
		WithRewriter(ReplacePrefix("/v", "/hidden/backend"))))
	if err != nil {
		t.Fatal(err)
	}
	const name = "/v/dir/file"

	single := []struct {
		op   string
		call func() error
	}{
		{"open", func() error { _, err := sfs.OpenFile(name, os.O_RDWR, 0); return err }},
		{"open", func() error { _, err := sfs.Open(name); return err }},
		{"open", func() error { _, err := sfs.Create(name); return err }},
		{"mkdir", func() error { return sfs.Mkdir(name, 0755) }},
		{"mkdir", func() error { return sfs.MkdirAll(name, 0755) }},
		{"remove", func() error { return sfs.Remove(name) }},
		{"RemoveAll", func() error { return sfs.RemoveAll(name) }},
		{"stat", func() error { _, err := sfs.Stat(name); return err }},
		{"lstat", func() error { _, err := sfs.Lstat(name); return err }},
		{"chmod", func() error { return sfs.Chmod(name, 0600) }},
		{"chtimes", func() error { return sfs.Chtimes(name, time.Now(), time.Now()) }},
		{"chown", func() error { return sfs.Chown(name, 1, 1) }},
		{"lchown", func() error { return sfs.Lchown(name, 1, 1) }},
		{"truncate", func() error { return sfs.Truncate(name, 0) }},
		{"open", func() error { _, err := sfs.ReadDir(name); return err }},
		{"open", func() error { _, err := sfs.ReadFile(name); return err }},
		{"readlink", func() error { _, err := sfs.Readlink(name); return err }},
		{"chdir", func() error { return sfs.Chdir(name) }},
	}
	for _, tt := range single {
		err := tt.call()
		pe, ok := err.(*os.PathError)
		if !ok {
			t.Errorf("%s: error %#v is not a *os.PathError", tt.op, err)
			continue
		}
		if pe.Path != name || pe.Op != tt.op || !errors.Is(err, errInjected) {
			t.Errorf("%s: got PathError{Op: %q, Path: %q, Err: %v}, want Op %q and Path %q",
				tt.op, pe.Op, pe.Path, pe.Err, tt.op, name)
		}
	}

	// Two-path methods report both caller paths in a LinkError.
	const other = "/v/other"
	for _, tt := range []struct {
		op   string
		call func(o, n string) error
		old  string
	}{
		{"rename", sfs.Rename, name},
		{"symlink", sfs.Symlink, "target/stays/as/given"},
	} {
		err := tt.call(tt.old, other)
		le, ok := err.(*os.LinkError)
		if !ok || le.Op != tt.op || le.Old != tt.old || le.New != other || !errors.Is(err, errInjected) {
			t.Errorf("%s: error = %#v, want LinkError{%s, %q, %q}", tt.op, err, tt.op, tt.old, other)
		}
	}
}
