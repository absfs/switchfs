package switchfs

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/absfs/osfs"
)

// TestEmptyName_FailsLikeOS checks that "" never means the working
// directory: every single-path method fails with ENOENT naming "", as
// package os does, and the backend is never called.
func TestEmptyName_FailsLikeOS(t *testing.T) {
	root := mustMemFS(t)
	sfs, err := New(WithDefault(root))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, sfs, "/keep.txt", "keep")

	ops := []struct {
		op   string
		call func() error
	}{
		{"stat", func() error { _, err := sfs.Stat(""); return err }},
		{"lstat", func() error { _, err := sfs.Lstat(""); return err }},
		{"open", func() error { _, err := sfs.Open(""); return err }},
		{"open", func() error { _, err := sfs.OpenFile("", os.O_RDWR|os.O_CREATE, 0644); return err }},
		{"open", func() error { _, err := sfs.Create(""); return err }},
		{"open", func() error { _, err := sfs.ReadDir(""); return err }},
		{"open", func() error { _, err := sfs.ReadFile(""); return err }},
		{"mkdir", func() error { return sfs.Mkdir("", 0755) }},
		{"mkdir", func() error { return sfs.MkdirAll("", 0755) }},
		{"remove", func() error { return sfs.Remove("") }},
		{"chmod", func() error { return sfs.Chmod("", 0600) }},
		{"chtimes", func() error { return sfs.Chtimes("", time.Now(), time.Now()) }},
		{"chown", func() error { return sfs.Chown("", 0, 0) }},
		{"truncate", func() error { return sfs.Truncate("", 0) }},
		{"lchown", func() error { return sfs.Lchown("", 0, 0) }},
		{"readlink", func() error { _, err := sfs.Readlink(""); return err }},
		{"chdir", func() error { return sfs.Chdir("") }},
		{"sub", func() error { _, err := sfs.Sub(""); return err }},
	}
	for _, tt := range ops {
		err := tt.call()
		pe, ok := err.(*os.PathError)
		if !ok || pe.Op != tt.op || pe.Path != "" || !errors.Is(err, syscall.ENOENT) || !os.IsNotExist(err) {
			t.Errorf("%s(\"\") = %#v, want PathError{%s, \"\", ENOENT}", tt.op, err, tt.op)
		}
	}

	for _, tt := range []struct {
		op       string
		old, new string
		call     func(o, n string) error
	}{
		{"rename", "", "/x", sfs.Rename},
		{"rename", "/keep.txt", "", sfs.Rename},
		{"symlink", "/keep.txt", "", sfs.Symlink},
	} {
		err := tt.call(tt.old, tt.new)
		le, ok := err.(*os.LinkError)
		if !ok || le.Op != tt.op || le.Old != tt.old || le.New != tt.new || !errors.Is(err, syscall.ENOENT) {
			t.Errorf("%s(%q, %q) = %#v", tt.op, tt.old, tt.new, err)
		}
	}

	// Nothing above touched the tree or the working directory.
	if got := readFile(t, sfs, "/keep.txt"); got != "keep" {
		t.Fatalf("/keep.txt = %q", got)
	}
	if entries, err := root.ReadDir("/"); err != nil || len(entries) != 1 {
		t.Fatalf("root entries = %v, %v", entries, err)
	}
	if wd, _ := sfs.Getwd(); wd != "/" {
		t.Fatalf("working directory moved to %q", wd)
	}

	// "." still means the working directory.
	if info, err := sfs.Stat("."); err != nil || !info.IsDir() {
		t.Fatalf("Stat(.) = %v, %v", info, err)
	}
	if _, err := sfs.Sub("."); err != nil {
		t.Fatalf("Sub(.) = %v", err)
	}
}

// TestRemoveAll_DotAndEmptyNames is the regression test for RemoveAll("")
// and RemoveAll(".") deleting the whole working directory tree.
func TestRemoveAll_DotAndEmptyNames(t *testing.T) {
	sfs, _, _ := workspaceFS(t, "ws")
	writeFile(t, sfs, "/keep.txt", "root")
	writeFile(t, sfs, "/ws/keep.txt", "ws")
	if err := sfs.Mkdir("/ws/a", 0755); err != nil {
		t.Fatal(err)
	}

	for _, cwd := range []string{"/", "/ws", "/ws/a"} {
		if err := sfs.Chdir(cwd); err != nil {
			t.Fatal(err)
		}
		if err := sfs.RemoveAll(""); err != nil {
			t.Errorf("cwd %s: RemoveAll(\"\") = %v, want nil (as os)", cwd, err)
		}
		for _, name := range []string{".", "./", "..", "a/..", "a/.", "/ws/.", "/ws/a/..", "../"} {
			err := sfs.RemoveAll(name)
			pe, ok := err.(*os.PathError)
			if !ok || pe.Op != "RemoveAll" || pe.Path != name || !errors.Is(err, syscall.EINVAL) {
				t.Errorf("cwd %s: RemoveAll(%q) = %#v, want EINVAL", cwd, name, err)
			}
		}
	}

	if got := readFile(t, sfs, "/keep.txt"); got != "root" {
		t.Fatalf("/keep.txt = %q", got)
	}
	if got := readFile(t, sfs, "/ws/keep.txt"); got != "ws" {
		t.Fatalf("/ws/keep.txt = %q", got)
	}
	if _, err := sfs.Stat("/ws/a"); err != nil {
		t.Fatalf("/ws/a removed: %v", err)
	}

	// A plain relative name still works.
	if err := sfs.Chdir("/ws"); err != nil {
		t.Fatal(err)
	}
	if err := sfs.RemoveAll("a"); err != nil {
		t.Fatal(err)
	}
	if _, err := sfs.Stat("/ws/a"); !os.IsNotExist(err) {
		t.Fatalf("/ws/a after RemoveAll(a) = %v", err)
	}
}

// TestRemoveAll_DotNamesOnHost repeats the reviewer's reproduction with an
// osfs route: with the working directory at a route root that maps onto a
// host directory, RemoveAll("") and RemoveAll(".") must not delete it.
func TestRemoveAll_DotNamesOnHost(t *testing.T) {
	skipOSFSOnWindows(t)
	host, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	dir := hostTempDir(t)
	precious := filepath.Join(filepath.FromSlash(dir), "precious.txt")
	if err := os.WriteFile(precious, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	sfs, err := New(
		WithDefault(mustMemFS(t)),
		WithRoute("/work", host, WithRewriter(ReplacePrefix("/work", dir))),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := sfs.Chdir("/work"); err != nil {
		t.Fatal(err)
	}
	if err := sfs.RemoveAll(""); err != nil {
		t.Fatalf("RemoveAll(\"\") = %v", err)
	}
	if err := sfs.RemoveAll("."); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("RemoveAll(.) = %v", err)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Fatalf("host file deleted: %v", err)
	}
}
