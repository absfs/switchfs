package switchfs

import (
	"errors"
	"os"
	"testing"

	"github.com/absfs/absfs"
)

// plainFS hides a backend's symlink methods, standing in for a backend
// without symlink support.
type plainFS struct{ absfs.FileSystem }

func TestSymlink_ThroughRewritingRoute(t *testing.T) {
	sfs, _, b := workspaceFS(t, "ws")
	ws := b["ws"]
	writeFile(t, sfs, "/ws/target.txt", "T")

	if err := sfs.Symlink("target.txt", "/ws/link"); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	// The link lives at the backend path, with its target stored verbatim.
	if got, err := ws.Readlink("/link"); err != nil || got != "target.txt" {
		t.Fatalf("backend Readlink(/link) = %q, %v", got, err)
	}
	if got, err := sfs.Readlink("/ws/link"); err != nil || got != "target.txt" {
		t.Fatalf("Readlink = %q, %v", got, err)
	}

	info, err := sfs.Lstat("/ws/link")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 || info.Name() != "link" {
		t.Fatalf("Lstat = %v %q", info.Mode(), info.Name())
	}

	// Stat and ReadFile follow a relative link inside the backend.
	if info, err := sfs.Stat("/ws/link"); err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("Stat should follow: %v, %v", info, err)
	}
	if got := readFile(t, sfs, "/ws/link"); got != "T" {
		t.Fatalf("ReadFile via link = %q", got)
	}

	if err := sfs.Lchown("/ws/link", 7, 8); err != nil {
		t.Fatalf("Lchown: %v", err)
	}
}

func TestSymlink_TargetsAreOpaque(t *testing.T) {
	sfs, _, b := workspaceFS(t, "ws")
	// An absolute target is stored as given, not rewritten into backend space.
	if err := sfs.Symlink("/ws/elsewhere", "/ws/abs"); err != nil {
		t.Fatal(err)
	}
	if got, _ := b["ws"].Readlink("/abs"); got != "/ws/elsewhere" {
		t.Fatalf("stored target = %q, want /ws/elsewhere", got)
	}
	if got, _ := sfs.Readlink("/ws/abs"); got != "/ws/elsewhere" {
		t.Fatalf("Readlink = %q", got)
	}
}

func TestSymlink_RelativeLinkName(t *testing.T) {
	sfs, _, b := workspaceFS(t, "ws")
	if err := sfs.Chdir("/ws"); err != nil {
		t.Fatal(err)
	}
	if err := sfs.Symlink("x", "rel"); err != nil {
		t.Fatal(err)
	}
	if got, err := b["ws"].Readlink("/rel"); err != nil || got != "x" {
		t.Fatalf("backend Readlink(/rel) = %q, %v", got, err)
	}
}

func TestSymlink_BackendWithoutSupport(t *testing.T) {
	m := mustMemFS(t)
	sfs, err := New(WithRoute("/p", plainFS{m}, WithRewriter(StripPrefix("/p"))))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/p/f", "x")

	// Lstat falls back to Stat.
	info, err := sfs.Lstat("/p/f")
	if err != nil || info.Size() != 1 {
		t.Fatalf("Lstat fallback = %v, %v", info, err)
	}

	unsupported := func(op string, err error) {
		t.Helper()
		if !errors.Is(err, ErrSymlinkNotSupported) || !errors.Is(err, errors.ErrUnsupported) {
			t.Fatalf("%s: %v does not wrap ErrSymlinkNotSupported", op, err)
		}
	}

	err = sfs.Lchown("/p/f", 1, 1)
	unsupported("Lchown", err)
	if pe, ok := err.(*os.PathError); !ok || pe.Path != "/p/f" || pe.Op != "lchown" {
		t.Fatalf("Lchown error = %#v", err)
	}

	_, err = sfs.Readlink("/p/f")
	unsupported("Readlink", err)
	if pe, ok := err.(*os.PathError); !ok || pe.Path != "/p/f" || pe.Op != "readlink" {
		t.Fatalf("Readlink error = %#v", err)
	}

	err = sfs.Symlink("f", "/p/l")
	unsupported("Symlink", err)
	if le, ok := err.(*os.LinkError); !ok || le.Old != "f" || le.New != "/p/l" {
		t.Fatalf("Symlink error = %#v", err)
	}
}

func TestSymlink_NoRoute(t *testing.T) {
	sfs, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sfs.Lstat("/x"); err != ErrNoRoute {
		t.Errorf("Lstat = %v", err)
	}
	if err := sfs.Lchown("/x", 0, 0); err != ErrNoRoute {
		t.Errorf("Lchown = %v", err)
	}
	if _, err := sfs.Readlink("/x"); err != ErrNoRoute {
		t.Errorf("Readlink = %v", err)
	}
	if err := sfs.Symlink("t", "/x"); err != ErrNoRoute {
		t.Errorf("Symlink = %v", err)
	}
}

func TestSymlink_BackendErrorsUseVirtualPaths(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")
	writeFile(t, sfs, "/ws/exists", "x")

	err := sfs.Symlink("t", "/ws/exists")
	le, ok := err.(*os.LinkError)
	if !ok || le.New != "/ws/exists" || le.Old != "t" || !os.IsExist(err) {
		t.Fatalf("Symlink over a file = %#v", err)
	}

	_, err = sfs.Readlink("/ws/missing/link")
	if err == nil {
		t.Fatal("Readlink of a missing path should fail")
	}
	var pe *os.PathError
	if errors.As(err, &pe) && pe.Path != "/ws/missing/link" {
		t.Fatalf("Readlink error path = %q", pe.Path)
	}
}

func TestSymlink_CrossBackendMove(t *testing.T) {
	src, dst := mustMemFS(t), mustMemFS(t)
	sfs, err := New(
		WithRoute("/a", src, WithRewriter(StripPrefix("/a"))),
		WithRoute("/b", dst, WithRewriter(StripPrefix("/b"))),
		WithRoute("/p", plainFS{mustMemFS(t)}, WithRewriter(StripPrefix("/p"))),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := sfs.Symlink("../somewhere", "/a/l"); err != nil {
		t.Fatal(err)
	}
	if err := sfs.Rename("/a/l", "/b/l"); err != nil {
		t.Fatalf("move symlink: %v", err)
	}
	if got, err := dst.Readlink("/l"); err != nil || got != "../somewhere" {
		t.Fatalf("moved link = %q, %v", got, err)
	}
	if _, err := src.Lstat("/l"); err == nil {
		t.Fatal("source link was not removed")
	}

	// A backend without symlinks can't receive one.
	if err := sfs.Symlink("x", "/b/l2"); err != nil {
		t.Fatal(err)
	}
	err = sfs.Rename("/b/l2", "/p/l2")
	if !errors.Is(err, ErrSymlinkNotSupported) {
		t.Fatalf("move to plain backend = %v", err)
	}
	if le, ok := err.(*os.LinkError); !ok || le.Old != "/b/l2" || le.New != "/p/l2" {
		t.Fatalf("error = %#v", err)
	}
}
