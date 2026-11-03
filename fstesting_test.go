package switchfs

import (
	"path/filepath"
	"runtime"
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

// The suites below run with rewriting routes. Their symlink sections are
// off: the suite links to absolute virtual paths, and switchfs stores link
// targets verbatim, so a backend following such a link looks for the
// virtual path in its own (rewritten) namespace. That is the documented
// behaviour; relative targets are covered in symlink_test.go.

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
