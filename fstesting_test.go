package switchfs

import (
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/fstesting"
	"github.com/absfs/memfs"
	"github.com/absfs/osfs"
)

// features is the fstesting feature set for memfs and osfs backends.
func features(symlinks bool) fstesting.Features {
	return fstesting.Features{
		Symlinks:      symlinks,
		HardLinks:     false,
		Permissions:   true,
		Timestamps:    true,
		CaseSensitive: true,
		AtomicRename:  true,
		SparseFiles:   false,
		LargeFiles:    false,
	}
}

func TestSwitchFS_WrapperSuite(t *testing.T) {
	baseFS, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create base filesystem: %v", err)
	}

	suite := &fstesting.WrapperSuite{
		Name:   "switchfs",
		BaseFS: baseFS,
		Factory: func(base absfs.FileSystem) (absfs.FileSystem, error) {
			return New(WithDefault(base))
		},
		TransformsData: false,
		TransformsMeta: false,
		ReadOnly:       false,
	}

	suite.Run(t)
}

func TestSwitchFS_Suite(t *testing.T) {
	backend, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create backend filesystem: %v", err)
	}

	fs, err := New(WithDefault(backend))
	if err != nil {
		t.Fatalf("failed to create switchfs: %v", err)
	}

	suite := &fstesting.Suite{FS: fs, Features: features(true)}
	suite.Run(t)
}

func TestSwitchFS_MultiBackend(t *testing.T) {
	hotBackend, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create hot backend: %v", err)
	}

	coldBackend, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("failed to create cold backend: %v", err)
	}

	fs, err := New(
		WithDefault(coldBackend),
		WithRoute("/hot", hotBackend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("failed to create switchfs: %v", err)
	}

	suite := &fstesting.Suite{FS: fs, Features: features(true)}
	suite.Run(t)
}

// The suites below run with rewriting routes. Their symlink groups are off:
// the suite links to absolute virtual paths, and switchfs stores link
// targets verbatim, so a backend following such a link looks for the
// virtual path in its own (rewritten) namespace. That is the documented
// behaviour. The SymlinkSuite tests further down rerun the symlink group
// through relLinks, which stores those targets as relative links.

// TestSwitchFS_Suite_StripPrefix runs the suite in a route that strips its
// prefix, so every operation reaches the backend under a different path.
func TestSwitchFS_Suite_StripPrefix(t *testing.T) {
	root := mustMemFS(t)
	tmp := mustMemFS(t)
	fs, err := New(
		WithDefault(root),
		WithRoute("/tmp", tmp, WithPriority(100), WithRewriter(StripPrefix("/tmp"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	suite := &fstesting.Suite{FS: fs, Features: features(false)}
	suite.Run(t)
}

// TestSwitchFS_Suite_WorkspaceRegex runs the suite inside a C4-style
// workspace: a regex route with StripPrefix under a memfs root listing.
func TestSwitchFS_Suite_WorkspaceRegex(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")
	suite := &fstesting.Suite{FS: sfs, Features: features(false), TestDir: "/ws"}
	suite.Run(t)
}

// TestSwitchFS_Suite_ReplacePrefix runs the suite where the backend keeps
// the data under another directory.
func TestSwitchFS_Suite_ReplacePrefix(t *testing.T) {
	backend := mustMemFS(t)
	if err := backend.MkdirAll("/storage", 0755); err != nil {
		t.Fatal(err)
	}
	fs, err := New(
		WithDefault(mustMemFS(t)),
		WithRoute("/data", backend, WithPriority(100), WithRewriter(ReplacePrefix("/data", "/storage"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.MkdirAll("/data", 0755); err != nil {
		t.Fatal(err)
	}
	suite := &fstesting.Suite{FS: fs, Features: features(false), TestDir: "/data"}
	suite.Run(t)
}

// hostTempDir returns a temp dir with symlinks resolved (macOS /var is a
// link to /private/var), as a slash path.
func hostTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(dir)
}

func skipOSFSOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("osfs suites use Unix host paths")
	}
}

// TestSwitchFS_Suite_OSFS runs the suite, symlinks included, against an osfs
// route without rewriting: virtual paths are host paths under a temp dir.
func TestSwitchFS_Suite_OSFS(t *testing.T) {
	skipOSFSOnWindows(t)
	host, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	dir := hostTempDir(t)
	fs, err := New(
		WithDefault(mustMemFS(t)),
		WithRoute(dir, host, WithPriority(100)),
	)
	if err != nil {
		t.Fatal(err)
	}
	suite := &fstesting.Suite{FS: fs, Features: features(true), TestDir: dir}
	suite.Run(t)
}

// TestSwitchFS_Suite_OSFSRewrite runs the suite against osfs through a route
// that maps /work onto a host temp dir.
func TestSwitchFS_Suite_OSFSRewrite(t *testing.T) {
	skipOSFSOnWindows(t)
	host, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	dir := hostTempDir(t)
	fs, err := New(
		WithDefault(mustMemFS(t)),
		WithRoute("/work", host, WithPriority(100), WithRewriter(ReplacePrefix("/work", dir))),
	)
	if err != nil {
		t.Fatal(err)
	}
	suite := &fstesting.Suite{FS: fs, Features: features(false), TestDir: "/work"}
	suite.Run(t)
}

// relLinks presents a SwitchFS to fstesting with absolute symlink targets
// stored as relative ones, the form the package documentation recommends.
// The suite links to absolute virtual paths, which a backend behind a
// rewriting route can't follow (targets are opaque); converting them lets
// the suite's symlink group run through rewriting routes too. Readlink
// returns a converted target as the suite wrote it.
type relLinks struct {
	*SwitchFS
	mu   sync.Mutex
	orig map[string]string // link path -> absolute target as given
}

func newRelLinks(sfs *SwitchFS) *relLinks {
	return &relLinks{SwitchFS: sfs, orig: map[string]string{}}
}

func (r *relLinks) Symlink(oldname, newname string) error {
	target := oldname
	if path.IsAbs(oldname) && path.IsAbs(newname) {
		target = relPath(path.Dir(path.Clean(newname)), path.Clean(oldname))
	}
	if err := r.SwitchFS.Symlink(target, newname); err != nil {
		return err
	}
	if target != oldname {
		r.mu.Lock()
		r.orig[path.Clean(newname)] = oldname
		r.mu.Unlock()
	}
	return nil
}

func (r *relLinks) Readlink(name string) (string, error) {
	target, err := r.SwitchFS.Readlink(name)
	if err != nil {
		return target, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if orig, ok := r.orig[path.Clean(name)]; ok {
		return orig, nil
	}
	return target, nil
}

func (r *relLinks) Rename(oldpath, newpath string) error {
	if err := r.SwitchFS.Rename(oldpath, newpath); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if orig, ok := r.orig[path.Clean(oldpath)]; ok {
		delete(r.orig, path.Clean(oldpath))
		r.orig[path.Clean(newpath)] = orig
	}
	return nil
}

func (r *relLinks) Remove(name string) error {
	r.mu.Lock()
	delete(r.orig, path.Clean(name))
	r.mu.Unlock()
	return r.SwitchFS.Remove(name)
}

// relPath returns the relative path from directory from to target, both
// clean absolute slash paths.
func relPath(from, target string) string {
	split := func(p string) []string {
		if p == "/" {
			return nil
		}
		return strings.Split(strings.TrimPrefix(p, "/"), "/")
	}
	f, t := split(from), split(target)
	i := 0
	for i < len(f) && i < len(t) && f[i] == t[i] {
		i++
	}
	var parts []string
	for range f[i:] {
		parts = append(parts, "..")
	}
	parts = append(parts, t[i:]...)
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

func TestRelPath(t *testing.T) {
	for _, tt := range []struct{ from, to, want string }{
		{"/a/b", "/a/b/c", "c"},
		{"/a/b", "/a/c", "../c"},
		{"/", "/x/y", "x/y"},
		{"/x/y", "/", "../.."},
		{"/a", "/a", "."},
	} {
		if got := relPath(tt.from, tt.to); got != tt.want {
			t.Errorf("relPath(%q, %q) = %q, want %q", tt.from, tt.to, got, tt.want)
		}
	}
}

// The suites below rerun the rewriting setups with the symlink group on,
// through relLinks.

func TestSwitchFS_SymlinkSuite_StripPrefix(t *testing.T) {
	fs, err := New(
		WithDefault(mustMemFS(t)),
		WithRoute("/tmp", mustMemFS(t), WithPriority(100), WithRewriter(StripPrefix("/tmp"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	suite := &fstesting.Suite{FS: newRelLinks(fs), Features: features(true)}
	suite.Run(t)
}

func TestSwitchFS_SymlinkSuite_WorkspaceRegex(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")
	suite := &fstesting.Suite{FS: newRelLinks(sfs), Features: features(true), TestDir: "/ws"}
	suite.Run(t)
}

func TestSwitchFS_SymlinkSuite_OSFSRewrite(t *testing.T) {
	skipOSFSOnWindows(t)
	host, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	dir := hostTempDir(t)
	fs, err := New(
		WithDefault(mustMemFS(t)),
		WithRoute("/work", host, WithPriority(100), WithRewriter(ReplacePrefix("/work", dir))),
	)
	if err != nil {
		t.Fatal(err)
	}
	suite := &fstesting.Suite{FS: newRelLinks(fs), Features: features(true), TestDir: "/work"}
	suite.Run(t)
}
