package switchfs

import (
	"errors"
	"os"
	"testing"

	"github.com/absfs/absfs"
)

var errInjected = errors.New("injected fault")

// faultFS is a memfs that fails the operations named in fail.
type faultFS struct {
	absfs.SymlinkFileSystem
	fail map[string]bool
	// closeFails makes files opened for writing fail on Close.
	closeFails bool
	// readFails makes files opened for reading fail on Read.
	readFails bool
}

func newFaultFS(t *testing.T, ops ...string) *faultFS {
	f := &faultFS{SymlinkFileSystem: mustMemFS(t), fail: map[string]bool{}}
	for _, op := range ops {
		f.fail[op] = true
	}
	return f
}

func (f *faultFS) err(op, name string) error {
	if f.fail[op] {
		return &os.PathError{Op: op, Path: name, Err: errInjected}
	}
	return nil
}

func (f *faultFS) Open(name string) (absfs.File, error) {
	if err := f.err("open", name); err != nil {
		return nil, err
	}
	file, err := f.SymlinkFileSystem.Open(name)
	if err == nil && f.readFails {
		return &errFile{name: name}, nil
	}
	return file, err
}

func (f *faultFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	if err := f.err("openfile", name); err != nil {
		return nil, err
	}
	file, err := f.SymlinkFileSystem.OpenFile(name, flag, perm)
	if err == nil && f.closeFails {
		return &closeFailFile{File: file}, nil
	}
	return file, err
}

func (f *faultFS) MkdirAll(name string, perm os.FileMode) error {
	if err := f.err("mkdirall", name); err != nil {
		return err
	}
	return f.SymlinkFileSystem.MkdirAll(name, perm)
}

func (f *faultFS) ReadDir(name string) ([]os.DirEntry, error) {
	if err := f.err("readdir", name); err != nil {
		return nil, err
	}
	return f.SymlinkFileSystem.ReadDir(name)
}

func (f *faultFS) Readlink(name string) (string, error) {
	if err := f.err("readlink", name); err != nil {
		return "", err
	}
	return f.SymlinkFileSystem.Readlink(name)
}

func (f *faultFS) Symlink(oldname, newname string) error {
	if err := f.err("symlink", newname); err != nil {
		return err
	}
	return f.SymlinkFileSystem.Symlink(oldname, newname)
}

func (f *faultFS) Stat(name string) (os.FileInfo, error) {
	if f.fail["stat"] {
		return nil, errInjected // a bare error, not a PathError
	}
	return f.SymlinkFileSystem.Stat(name)
}

type closeFailFile struct{ absfs.File }

func (c *closeFailFile) Close() error {
	c.File.Close()
	return errInjected
}

// moveFixture routes /src and /dst to the given backends with StripPrefix.
func moveFixture(t *testing.T, src, dst absfs.FileSystem) *SwitchFS {
	t.Helper()
	sfs, err := New(
		WithRoute("/src", src, WithRewriter(StripPrefix("/src"))),
		WithRoute("/dst", dst, WithRewriter(StripPrefix("/dst"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	return sfs
}

func TestCrossBackendMove_Faults(t *testing.T) {
	tests := []struct {
		name  string
		setup func(src, dst *faultFS)
		build func(t *testing.T, sfs *SwitchFS)
	}{
		{"source open fails", func(src, dst *faultFS) { src.fail["open"] = true }, buildFile},
		{"destination create fails", func(src, dst *faultFS) { dst.fail["openfile"] = true }, buildFile},
		{"copy fails", func(src, dst *faultFS) { src.readFails = true }, buildFile},
		{"destination close fails", func(src, dst *faultFS) { dst.closeFails = true }, buildFile},
		{"destination mkdir fails", func(src, dst *faultFS) { dst.fail["mkdirall"] = true }, buildDir},
		{"source readdir fails", func(src, dst *faultFS) { src.fail["readdir"] = true }, buildDir},
		{"child move fails", func(src, dst *faultFS) { dst.fail["openfile"] = true }, buildDir},
		{"readlink fails", func(src, dst *faultFS) { src.fail["readlink"] = true }, buildLink},
		{"symlink fails", func(src, dst *faultFS) { dst.fail["symlink"] = true }, buildLink},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst := newFaultFS(t), newFaultFS(t)
			sfs := moveFixture(t, src, dst)
			tt.build(t, sfs)
			tt.setup(src, dst)

			err := sfs.Rename("/src/item", "/dst/item")
			// errFile, used when reads fail, reports os.ErrPermission.
			if !errors.Is(err, errInjected) && !(src.readFails && errors.Is(err, os.ErrPermission)) {
				t.Fatalf("Rename = %v, want the injected fault", err)
			}
			le, ok := err.(*os.LinkError)
			if !ok || le.Old != "/src/item" || le.New != "/dst/item" {
				t.Fatalf("error = %#v, want a LinkError with virtual paths", err)
			}
			// The source is never removed when the copy fails.
			if _, err := src.SymlinkFileSystem.Lstat("/item"); err != nil {
				t.Fatalf("source removed after failed move: %v", err)
			}
		})
	}
}

func buildFile(t *testing.T, sfs *SwitchFS) { writeFile(t, sfs, "/src/item", "data") }

func buildDir(t *testing.T, sfs *SwitchFS) {
	if err := sfs.MkdirAll("/src/item", 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/src/item/f", "data")
}

func buildLink(t *testing.T, sfs *SwitchFS) {
	if err := sfs.Symlink("target", "/src/item"); err != nil {
		t.Fatal(err)
	}
}

func TestRename_ResolveErrors(t *testing.T) {
	sfs, err := New(WithRoute("/a", mustMemFS(t)))
	if err != nil {
		t.Fatal(err)
	}
	if err := sfs.Rename("/a/x", "/nowhere/x"); err != ErrNoRoute {
		t.Fatalf("unrouted newpath = %v", err)
	}
	if err := sfs.Rename("/nowhere/x", "/a/x"); err != ErrNoRoute {
		t.Fatalf("unrouted oldpath = %v", err)
	}
}

func TestReadOps_NoRoute(t *testing.T) {
	sfs, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sfs.ReadDir("/x"); err != ErrNoRoute {
		t.Errorf("ReadDir = %v", err)
	}
	if _, err := sfs.ReadFile("/x"); err != ErrNoRoute {
		t.Errorf("ReadFile = %v", err)
	}
	if _, err := sfs.Sub("/x"); err != ErrNoRoute {
		t.Errorf("Sub = %v", err)
	}
	if err := sfs.Chdir("/x"); err != ErrNoRoute {
		t.Errorf("Chdir = %v", err)
	}
}

func TestChdir_Errors(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")
	writeFile(t, sfs, "/ws/file", "x")

	err := sfs.Chdir("/ws/missing")
	pe, ok := err.(*os.PathError)
	if !ok || pe.Op != "chdir" || pe.Path != "/ws/missing" || !os.IsNotExist(err) {
		t.Fatalf("Chdir(missing) = %#v", err)
	}
	err = sfs.Chdir("/ws/file")
	pe, ok = err.(*os.PathError)
	if !ok || pe.Op != "chdir" || pe.Path != "/ws/file" {
		t.Fatalf("Chdir(file) = %#v", err)
	}
	if wd, _ := sfs.Getwd(); wd != "/" {
		t.Fatalf("failed Chdir moved the working directory to %q", wd)
	}

	// A backend error that isn't a PathError passes through.
	ff := newFaultFS(t, "stat")
	sfs2, err := New(WithDefault(ff))
	if err != nil {
		t.Fatal(err)
	}
	if err := sfs2.Chdir("/"); err != errInjected {
		t.Fatalf("Chdir with failing Stat = %v", err)
	}
}

// errRouter fails every lookup with an error other than ErrNoRoute.
type errRouter struct{ plainRouter }

var errRouterBroken = errors.New("router broken")

func (errRouter) RouteWithInfo(string, os.FileInfo) (*Route, error) { return nil, errRouterBroken }

func TestRouterErrorsPropagate(t *testing.T) {
	sfs, err := New(WithRouter(&errRouter{plainRouter{inner: NewRouter()}}), WithDefault(mustMemFS(t)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sfs.Stat("/x"); err != errRouterBroken {
		t.Fatalf("Stat = %v, want the router's error (not the default backend)", err)
	}
}

func TestReadOps_BackendErrors(t *testing.T) {
	ff := newFaultFS(t, "readdir")
	sfs, err := New(WithRoute("/f", ff, WithRewriter(StripPrefix("/f"))))
	if err != nil {
		t.Fatal(err)
	}
	_, err = sfs.ReadDir("/f/d")
	if pe, ok := err.(*os.PathError); !ok || pe.Path != "/f/d" {
		t.Fatalf("ReadDir error = %#v", err)
	}
}
