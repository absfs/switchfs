package switchfs

import (
	"errors"
	"io"
	iofs "io/fs"
	"os"
	"sort"
	"testing"
	"testing/fstest"
	"time"

	"github.com/absfs/absfs"
)

// workspaceFS builds the C4 layout: a memfs root listing the workspace
// directories, and one regex route per workspace that strips its prefix.
func workspaceFS(t *testing.T, names ...string) (*SwitchFS, absfs.FileSystem, map[string]absfs.SymlinkFileSystem) {
	t.Helper()
	root := mustMemFS(t)
	sfs, err := New(WithDefault(root))
	if err != nil {
		t.Fatal(err)
	}
	backends := map[string]absfs.SymlinkFileSystem{}
	for _, name := range names {
		b := mustMemFS(t)
		backends[name] = b
		if err := root.Mkdir("/"+name, 0755); err != nil {
			t.Fatal(err)
		}
		err := sfs.AddRoute(`^/`+name+`(/|$)`, b,
			WithPatternType(PatternRegex),
			WithPriority(100),
			WithRewriter(StripPrefix("/"+name)))
		if err != nil {
			t.Fatal(err)
		}
	}
	return sfs, root, backends
}

func TestRewrite_AllOperationsReachBackendPath(t *testing.T) {
	sfs, root, b := workspaceFS(t, "ws")
	ws := b["ws"]

	// Create and write
	writeFile(t, sfs, "/ws/a.txt", "hello")
	if got := readFile(t, ws, "/a.txt"); got != "hello" {
		t.Fatalf("backend /a.txt = %q", got)
	}
	if _, err := root.Stat("/ws/a.txt"); err == nil {
		t.Fatal("file leaked into the default backend")
	}

	// ReadFile
	if got := readFile(t, sfs, "/ws/a.txt"); got != "hello" {
		t.Fatalf("ReadFile = %q", got)
	}

	// Mkdir / MkdirAll
	if err := sfs.Mkdir("/ws/d", 0755); err != nil {
		t.Fatal(err)
	}
	if err := sfs.MkdirAll("/ws/d/e/f", 0755); err != nil {
		t.Fatal(err)
	}
	if info, err := ws.Stat("/d/e/f"); err != nil || !info.IsDir() {
		t.Fatalf("backend /d/e/f: %v %v", info, err)
	}

	// Stat
	info, err := sfs.Stat("/ws/a.txt")
	if err != nil || info.Size() != 5 || info.Name() != "a.txt" {
		t.Fatalf("Stat = %v, %v", info, err)
	}

	// Chmod / Chtimes / Chown / Truncate
	if err := sfs.Chmod("/ws/a.txt", 0600); err != nil {
		t.Fatal(err)
	}
	if info, _ := ws.Stat("/a.txt"); info.Mode().Perm() != 0600 {
		t.Fatalf("Chmod not applied: %v", info.Mode())
	}
	mtime := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := sfs.Chtimes("/ws/a.txt", mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if info, _ := ws.Stat("/a.txt"); !info.ModTime().Equal(mtime) {
		t.Fatalf("Chtimes not applied: %v", info.ModTime())
	}
	if err := sfs.Chown("/ws/a.txt", 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := sfs.Truncate("/ws/a.txt", 2); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, ws, "/a.txt"); got != "he" {
		t.Fatalf("Truncate not applied: %q", got)
	}

	// ReadDir
	entries, err := sfs.ReadDir("/ws")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "a.txt" || names[1] != "d" {
		t.Fatalf("ReadDir(/ws) = %v", names)
	}

	// Rename within the route
	if err := sfs.Rename("/ws/a.txt", "/ws/d/b.txt"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, ws, "/d/b.txt"); got != "he" {
		t.Fatalf("renamed content = %q", got)
	}

	// Remove / RemoveAll
	if err := sfs.Remove("/ws/d/b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := sfs.RemoveAll("/ws/d"); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.Stat("/d"); !os.IsNotExist(err) {
		t.Fatalf("RemoveAll left /d: %v", err)
	}
}

func TestRewrite_RouteRootBecomesBackendRoot(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")

	// StripPrefix("/ws") turns "/ws" into "", which must reach the backend as "/".
	info, err := sfs.Stat("/ws")
	if err != nil {
		t.Fatalf("Stat(/ws): %v", err)
	}
	if !info.IsDir() {
		t.Fatal("route root is not a directory")
	}
	if info.Name() != "ws" {
		t.Fatalf("route root Name = %q, want ws", info.Name())
	}
	if _, err := sfs.ReadDir("/ws"); err != nil {
		t.Fatalf("ReadDir(/ws): %v", err)
	}
	if info, err := sfs.Lstat("/ws/"); err != nil || info.Name() != "ws" {
		t.Fatalf("Lstat(/ws/) = %v, %v", info, err)
	}

	// The root of the whole tree is "/".
	if info, err := sfs.Stat("/"); err != nil || info.Name() != "/" {
		t.Fatalf("Stat(/) = %v, %v", info, err)
	}
}

func TestRewrite_EmptyRewriteFromCustomRewriter(t *testing.T) {
	backend := mustMemFS(t)
	sfs, err := New(WithRoute("/x", backend, WithRewriter(StaticMapping(map[string]string{"/x": ""}))))
	if err != nil {
		t.Fatal(err)
	}
	backendPath := ""
	_, backendPath, err = sfs.getBackendAndRewrite("/x", nil)
	if err != nil || backendPath != "/" {
		t.Fatalf("rewrite of /x = %q, %v; want /", backendPath, err)
	}
}

func TestRewrite_FileNamesAreVirtual(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")
	writeFile(t, sfs, "/ws/a.txt", "abc")

	f, err := sfs.Open("/ws/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Name() != "/ws/a.txt" {
		t.Fatalf("Name = %q", f.Name())
	}
	info, err := f.Stat()
	if err != nil || info.Name() != "a.txt" {
		t.Fatalf("file Stat = %v, %v", info, err)
	}

	d, err := sfs.Open("/ws")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.Name() != "/ws" {
		t.Fatalf("dir Name = %q", d.Name())
	}
	if info, err := d.Stat(); err != nil || info.Name() != "ws" {
		t.Fatalf("dir Stat = %v, %v", info, err)
	}

	// A relative open keeps the caller's spelling, like os.File.Name.
	if err := sfs.Chdir("/ws"); err != nil {
		t.Fatal(err)
	}
	r, err := sfs.Open("a.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.Name() != "a.txt" {
		t.Fatalf("relative Name = %q", r.Name())
	}
}

func TestRewrite_UnrewrittenFilesAreNotWrapped(t *testing.T) {
	backend := mustMemFS(t)
	sfs, err := New(WithDefault(backend))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/plain.txt", "x")
	f, err := sfs.Open("/plain.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, wrapped := f.(*file); wrapped {
		t.Fatal("file whose name already matches should not be wrapped")
	}
}

func TestRewrite_ErrorsCarryVirtualPaths(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")

	// check requires a not-exist error and, when the backend reported a
	// PathError, that it names the caller's path. (memfs reports some
	// failures as a bare errno, which has no path to leak and passes through.)
	check := func(op string, err error, want string) {
		t.Helper()
		if !os.IsNotExist(err) {
			t.Fatalf("%s: %v is not IsNotExist", op, err)
		}
		var pe *os.PathError
		if errors.As(err, &pe) && pe.Path != want {
			t.Fatalf("%s: PathError.Path = %q, want %q", op, pe.Path, want)
		}
	}
	mustPathError := func(op string, err error) {
		t.Helper()
		if _, ok := err.(*os.PathError); !ok {
			t.Fatalf("%s: error %v (%T) is not a PathError", op, err, err)
		}
	}

	_, err := sfs.Stat("/ws/missing")
	check("Stat", err, "/ws/missing")
	mustPathError("Stat", err)
	_, err = sfs.Open("/ws/missing")
	check("Open", err, "/ws/missing")
	mustPathError("Open", err)
	_, err = sfs.ReadFile("/ws/missing")
	check("ReadFile", err, "/ws/missing")
	_, err = sfs.ReadDir("/ws/missing")
	check("ReadDir", err, "/ws/missing")
	_, err = sfs.Lstat("/ws/missing")
	check("Lstat", err, "/ws/missing")
	mustPathError("Lstat", err)
	check("Chmod", sfs.Chmod("/ws/missing", 0644), "/ws/missing")
	check("Chtimes", sfs.Chtimes("/ws/missing", time.Now(), time.Now()), "/ws/missing")
	check("Chown", sfs.Chown("/ws/missing", 1, 1), "/ws/missing")
	check("Remove", sfs.Remove("/ws/missing"), "/ws/missing")
	check("Truncate", sfs.Truncate("/ws/missing", 0), "/ws/missing")
	check("Mkdir", sfs.Mkdir("/ws/missing/sub", 0755), "/ws/missing/sub")

	// Relative names are reported as given.
	if err := sfs.Chdir("/ws"); err != nil {
		t.Fatal(err)
	}
	_, err = sfs.Stat("missing")
	check("Stat relative", err, "missing")

	// Rename reports both virtual paths in a LinkError.
	err = sfs.Rename("/ws/missing", "/ws/other")
	var le *os.LinkError
	if !errors.As(err, &le) || le.Old != "/ws/missing" || le.New != "/ws/other" {
		t.Fatalf("Rename error = %#v", err)
	}
	if !os.IsNotExist(err) {
		t.Fatalf("Rename error %v is not IsNotExist", err)
	}
}

func TestRewrite_RelativePathsUseVirtualCwd(t *testing.T) {
	sfs, root, b := workspaceFS(t, "ws")
	if err := sfs.Chdir("/ws"); err != nil {
		t.Fatal(err)
	}
	if wd, _ := sfs.Getwd(); wd != "/ws" {
		t.Fatalf("Getwd = %q", wd)
	}
	writeFile(t, sfs, "rel.txt", "r")
	if got := readFile(t, b["ws"], "/rel.txt"); got != "r" {
		t.Fatalf("backend /rel.txt = %q", got)
	}

	// ".." leaves the route and lands in the default backend.
	writeFile(t, sfs, "../top.txt", "t")
	if got := readFile(t, root, "/top.txt"); got != "t" {
		t.Fatalf("default /top.txt = %q", got)
	}
	if _, err := b["ws"].Stat("/top.txt"); err == nil {
		t.Fatal("../top.txt was written into the workspace")
	}

	// Paths are cleaned before routing, so ".." can't smuggle a path into
	// a route that its clean form doesn't match.
	writeFile(t, sfs, "/ws/../top2.txt", "t2")
	if got := readFile(t, root, "/top2.txt"); got != "t2" {
		t.Fatalf("default /top2.txt = %q", got)
	}
}

func TestRewrite_RenameAcrossRoutesSameBackend(t *testing.T) {
	shared := &countingRenameFS{FileSystem: mustMemFS(t)}
	sfs, err := New(
		WithRoute("/x", shared, WithRewriter(ReplacePrefix("/x", "/xx"))),
		WithRoute("/y", shared, WithRewriter(ReplacePrefix("/y", "/yy"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := shared.MkdirAll("/xx", 0755); err != nil {
		t.Fatal(err)
	}
	if err := shared.MkdirAll("/yy", 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/x/f", "data")
	if err := sfs.Rename("/x/f", "/y/f"); err != nil {
		t.Fatal(err)
	}
	if shared.renames != 1 {
		t.Fatalf("native renames = %d, want 1", shared.renames)
	}
	if got := readFile(t, shared, "/yy/f"); got != "data" {
		t.Fatalf("/yy/f = %q", got)
	}
}

type countingRenameFS struct {
	absfs.FileSystem
	renames int
}

func (c *countingRenameFS) Rename(o, n string) error {
	c.renames++
	return c.FileSystem.Rename(o, n)
}

func TestRewrite_CrossBackendMoveUsesBackendPaths(t *testing.T) {
	sfs, _, b := workspaceFS(t, "src", "dst")
	src, dst := b["src"], b["dst"]

	if err := sfs.MkdirAll("/src/tree/sub", 0750); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/src/tree/one.txt", "1")
	writeFile(t, sfs, "/src/tree/sub/two.txt", "22")
	if err := sfs.Chmod("/src/tree/one.txt", 0640); err != nil {
		t.Fatal(err)
	}
	mtime := time.Date(2021, 5, 6, 7, 8, 9, 0, time.UTC)
	if err := sfs.Chtimes("/src/tree/one.txt", mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := sfs.Symlink("one.txt", "/src/tree/link"); err != nil {
		t.Fatal(err)
	}

	if err := sfs.Rename("/src/tree", "/dst/moved"); err != nil {
		t.Fatalf("cross-backend Rename: %v", err)
	}

	if got := readFile(t, dst, "/moved/one.txt"); got != "1" {
		t.Fatalf("/moved/one.txt = %q", got)
	}
	if got := readFile(t, dst, "/moved/sub/two.txt"); got != "22" {
		t.Fatalf("/moved/sub/two.txt = %q", got)
	}
	info, err := dst.Stat("/moved/one.txt")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Errorf("mode not kept: %v", info.Mode())
	}
	if !info.ModTime().Equal(mtime) {
		t.Errorf("mtime not kept: %v", info.ModTime())
	}
	if target, err := dst.Readlink("/moved/link"); err != nil || target != "one.txt" {
		t.Errorf("symlink not recreated: %q, %v", target, err)
	}
	if li, err := dst.Lstat("/moved/link"); err != nil || li.Mode()&os.ModeSymlink == 0 {
		t.Errorf("moved link is not a symlink: %v, %v", li, err)
	}
	if _, err := src.Stat("/tree"); !os.IsNotExist(err) {
		t.Errorf("source tree still exists: %v", err)
	}

	// A single file, through the virtual tree.
	writeFile(t, sfs, "/src/single.txt", "s")
	if err := sfs.Rename("/src/single.txt", "/dst/single.txt"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, sfs, "/dst/single.txt"); got != "s" {
		t.Fatalf("/dst/single.txt = %q", got)
	}
}

func TestRewrite_Sub(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")
	writeFile(t, sfs, "/ws/a.txt", "A")
	if err := sfs.MkdirAll("/ws/dir", 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/ws/dir/b.txt", "B")

	sub, err := sfs.Sub("/ws")
	if err != nil {
		t.Fatal(err)
	}
	if err := fstest.TestFS(sub, "a.txt", "dir/b.txt"); err != nil {
		t.Fatal(err)
	}

	// A Sub of the whole tree keeps routing into the workspace.
	top, err := sfs.Sub("/")
	if err != nil {
		t.Fatal(err)
	}
	data, err := iofs.ReadFile(top, "ws/dir/b.txt")
	if err != nil || string(data) != "B" {
		t.Fatalf("ReadFile(ws/dir/b.txt) = %q, %v", data, err)
	}

	if _, err := sfs.Sub("/ws/a.txt"); err == nil {
		t.Fatal("Sub of a file should fail")
	}
}

// errFile is an absfs.File whose every method fails with a PathError naming
// a backend path, to check that the wrapper renames them all.
type errFile struct{ name string }

func (e *errFile) perr(op string) error {
	return &os.PathError{Op: op, Path: e.name, Err: os.ErrPermission}
}
func (e *errFile) Name() string                         { return e.name }
func (e *errFile) Read([]byte) (int, error)             { return 0, e.perr("read") }
func (e *errFile) ReadAt([]byte, int64) (int, error)    { return 0, e.perr("readat") }
func (e *errFile) Write([]byte) (int, error)            { return 0, e.perr("write") }
func (e *errFile) WriteAt([]byte, int64) (int, error)   { return 0, e.perr("writeat") }
func (e *errFile) WriteString(string) (int, error)      { return 0, e.perr("writestring") }
func (e *errFile) Seek(int64, int) (int64, error)       { return 0, e.perr("seek") }
func (e *errFile) Close() error                         { return e.perr("close") }
func (e *errFile) Sync() error                          { return e.perr("sync") }
func (e *errFile) Truncate(int64) error                 { return e.perr("truncate") }
func (e *errFile) Stat() (os.FileInfo, error)           { return nil, e.perr("stat") }
func (e *errFile) Readdir(int) ([]os.FileInfo, error)   { return nil, e.perr("readdir") }
func (e *errFile) Readdirnames(int) ([]string, error)   { return nil, e.perr("readdirnames") }
func (e *errFile) ReadDir(int) ([]iofs.DirEntry, error) { return nil, e.perr("readdir") }

func TestFileWrapper_TranslatesErrors(t *testing.T) {
	f := wrapFile(&errFile{name: "/backend/x"}, "/ws/x", "/ws/x")
	if f.Name() != "/ws/x" {
		t.Fatalf("Name = %q", f.Name())
	}
	buf := make([]byte, 1)
	errs := map[string]error{}
	_, errs["Read"] = f.Read(buf)
	_, errs["ReadAt"] = f.ReadAt(buf, 0)
	_, errs["Write"] = f.Write(buf)
	_, errs["WriteAt"] = f.WriteAt(buf, 0)
	_, errs["WriteString"] = f.WriteString("x")
	_, errs["Seek"] = f.Seek(0, io.SeekStart)
	errs["Close"] = f.Close()
	errs["Sync"] = f.Sync()
	errs["Truncate"] = f.Truncate(0)
	_, errs["Stat"] = f.Stat()
	_, errs["Readdir"] = f.Readdir(-1)
	_, errs["Readdirnames"] = f.Readdirnames(-1)
	_, errs["ReadDir"] = f.ReadDir(-1)
	for op, err := range errs {
		var pe *os.PathError
		if !errors.As(err, &pe) || pe.Path != "/ws/x" || !errors.Is(err, os.ErrPermission) {
			t.Errorf("%s: error %v not translated", op, err)
		}
	}
}

func TestFileWrapper_PassesThroughSuccess(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")
	if err := sfs.MkdirAll("/ws/d", 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/ws/d/a", "a")
	writeFile(t, sfs, "/ws/d/b", "b")

	f, err := sfs.OpenFile("/ws/f.txt", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := f.(*file); !ok {
		t.Fatalf("expected a wrapped file, got %T", f)
	}
	if _, err := f.WriteString("hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("J"), 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(f, buf); err != nil || string(buf) != "Jello" {
		t.Fatalf("Read = %q, %v", buf, err)
	}
	if _, err := f.ReadAt(buf[:2], 3); err != nil || string(buf[:2]) != "lo" {
		t.Fatalf("ReadAt = %q, %v", buf[:2], err)
	}
	if _, err := f.Read(buf); err != io.EOF {
		t.Fatalf("Read at EOF = %v, want io.EOF unchanged", err)
	}
	if err := f.Truncate(1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	for _, list := range []func(absfs.File) (int, error){
		func(d absfs.File) (int, error) { infos, err := d.Readdir(-1); return len(infos), err },
		func(d absfs.File) (int, error) { names, err := d.Readdirnames(-1); return len(names), err },
		func(d absfs.File) (int, error) { ents, err := d.ReadDir(-1); return len(ents), err },
	} {
		d, err := sfs.Open("/ws/d")
		if err != nil {
			t.Fatal(err)
		}
		n, err := list(d)
		if err != nil || n != 2 {
			t.Fatalf("listing = %d, %v", n, err)
		}
		d.Close()
	}
}

func TestWrapHelpers(t *testing.T) {
	if wrapFile(nil, "a", "/a") != nil {
		t.Fatal("wrapFile(nil) should be nil")
	}
	if wrapInfo(nil, "/a") != nil {
		t.Fatal("wrapInfo(nil) should be nil")
	}
	if pathError(nil, "x") != nil {
		t.Fatal("pathError(nil) should be nil")
	}
	plain := errors.New("plain")
	if pathError(plain, "x") != plain {
		t.Fatal("non-PathError should pass through")
	}
	same := &os.PathError{Op: "op", Path: "x", Err: plain}
	if pathError(same, "x") != same {
		t.Fatal("PathError already naming x should pass through")
	}

	le := linkError(&os.LinkError{Op: "rename", Old: "/b/o", New: "/b/n", Err: plain}, "rename", "o", "n").(*os.LinkError)
	if le.Old != "o" || le.New != "n" || le.Err != plain {
		t.Fatalf("LinkError not translated: %#v", le)
	}
	le = linkError(&os.PathError{Op: "open", Path: "/b/o", Err: plain}, "rename", "o", "n").(*os.LinkError)
	if le.Op != "rename" || le.Old != "o" || le.Err != plain {
		t.Fatalf("PathError not converted: %#v", le)
	}
	le = linkError(plain, "symlink", "t", "n").(*os.LinkError)
	if le.Op != "symlink" || le.Err != plain {
		t.Fatalf("plain error not wrapped: %#v", le)
	}
}

// sliceFS is a backend value type that isn't comparable.
type sliceFS struct {
	absfs.FileSystem
	tags []string
}

func TestSameBackend(t *testing.T) {
	m := mustMemFS(t)
	if !sameBackend(m, m) {
		t.Fatal("same pointer should be the same backend")
	}
	if sameBackend(m, mustMemFS(t)) {
		t.Fatal("different memfs should differ")
	}
	if sameBackend(nil, m) || sameBackend(m, nil) {
		t.Fatal("nil is never the same backend")
	}
	a := sliceFS{FileSystem: m, tags: []string{"a"}}
	if sameBackend(a, a) {
		t.Fatal("non-comparable backends must not be compared (and must not panic)")
	}
	if sameBackend(m, &countingRenameFS{FileSystem: m}) {
		t.Fatal("different types are different backends")
	}
}

func TestRename_NonComparableBackendsUseCopy(t *testing.T) {
	m1, m2 := mustMemFS(t), mustMemFS(t)
	sfs, err := New(
		WithRoute("/a", sliceFS{FileSystem: m1, tags: []string{"a"}}, WithRewriter(StripPrefix("/a"))),
		WithRoute("/b", sliceFS{FileSystem: m2, tags: []string{"b"}}, WithRewriter(StripPrefix("/b"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/a/f", "x")
	if err := sfs.Rename("/a/f", "/b/f"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, m2, "/f"); got != "x" {
		t.Fatalf("moved file = %q", got)
	}
}
