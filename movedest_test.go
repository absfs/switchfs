package switchfs

import (
	"errors"
	iofs "io/fs"
	"os"
	"syscall"
	"testing"
)

// TestCrossBackendMove_ExistingDestination checks that a move between
// backends treats an existing destination as rename(2) does.
func TestCrossBackendMove_ExistingDestination(t *testing.T) {
	type fixture struct {
		sfs      *SwitchFS
		src, dst *faultFS
	}
	setup := func(t *testing.T) fixture {
		src, dst := newFaultFS(t), newFaultFS(t)
		return fixture{moveFixture(t, src, dst), src, dst}
	}
	mkdir := func(t *testing.T, sfs *SwitchFS, name string) {
		t.Helper()
		if err := sfs.MkdirAll(name, 0755); err != nil {
			t.Fatal(err)
		}
	}
	link := func(t *testing.T, sfs *SwitchFS, target, name string) {
		t.Helper()
		if err := sfs.Symlink(target, name); err != nil {
			t.Fatal(err)
		}
	}
	gone := func(t *testing.T, sfs *SwitchFS, name string) {
		t.Helper()
		if _, err := sfs.Lstat(name); !os.IsNotExist(err) {
			t.Fatalf("%s still exists after the move: %v", name, err)
		}
	}
	isLink := func(t *testing.T, sfs *SwitchFS, name, want string) {
		t.Helper()
		got, err := sfs.Readlink(name)
		if err != nil || got != want {
			t.Fatalf("Readlink(%s) = %q, %v, want %q", name, got, err, want)
		}
	}

	t.Run("symlink replaces a file", func(t *testing.T) {
		f := setup(t)
		link(t, f.sfs, "a.txt", "/src/item")
		writeFile(t, f.sfs, "/dst/item", "existing")
		if err := f.sfs.Rename("/src/item", "/dst/item"); err != nil {
			t.Fatalf("Rename = %v", err)
		}
		isLink(t, f.sfs, "/dst/item", "a.txt")
		gone(t, f.sfs, "/src/item")
	})

	t.Run("symlink replaces a symlink", func(t *testing.T) {
		f := setup(t)
		link(t, f.sfs, "new", "/src/item")
		link(t, f.sfs, "old", "/dst/item")
		if err := f.sfs.Rename("/src/item", "/dst/item"); err != nil {
			t.Fatalf("Rename = %v", err)
		}
		isLink(t, f.sfs, "/dst/item", "new")
	})

	t.Run("file replaces a file", func(t *testing.T) {
		f := setup(t)
		writeFile(t, f.sfs, "/src/item", "new")
		writeFile(t, f.sfs, "/dst/item", "old and longer")
		if err := f.sfs.Rename("/src/item", "/dst/item"); err != nil {
			t.Fatalf("Rename = %v", err)
		}
		if got := readFile(t, f.sfs, "/dst/item"); got != "new" {
			t.Fatalf("/dst/item = %q", got)
		}
	})

	t.Run("file replaces a symlink, not its target", func(t *testing.T) {
		f := setup(t)
		writeFile(t, f.sfs, "/src/item", "new")
		writeFile(t, f.sfs, "/dst/target", "untouched")
		link(t, f.sfs, "target", "/dst/item")
		if err := f.sfs.Rename("/src/item", "/dst/item"); err != nil {
			t.Fatalf("Rename = %v", err)
		}
		info, err := f.sfs.Lstat("/dst/item")
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("/dst/item = %v, %v, want a regular file", info, err)
		}
		if got := readFile(t, f.sfs, "/dst/target"); got != "untouched" {
			t.Fatalf("link target written through: %q", got)
		}
	})

	t.Run("directory replaces an empty directory", func(t *testing.T) {
		f := setup(t)
		mkdir(t, f.sfs, "/src/item")
		writeFile(t, f.sfs, "/src/item/f", "data")
		mkdir(t, f.sfs, "/dst/item")
		if err := f.sfs.Rename("/src/item", "/dst/item"); err != nil {
			t.Fatalf("Rename = %v", err)
		}
		if got := readFile(t, f.sfs, "/dst/item/f"); got != "data" {
			t.Fatalf("/dst/item/f = %q", got)
		}
		gone(t, f.sfs, "/src/item")
	})

	refused := []struct {
		name  string
		build func(t *testing.T, sfs *SwitchFS)
		want  error
	}{
		{"file onto a directory", func(t *testing.T, sfs *SwitchFS) {
			writeFile(t, sfs, "/src/item", "data")
			mkdir(t, sfs, "/dst/item")
		}, syscall.EISDIR},
		{"symlink onto a directory", func(t *testing.T, sfs *SwitchFS) {
			link(t, sfs, "x", "/src/item")
			mkdir(t, sfs, "/dst/item")
		}, syscall.EISDIR},
		{"directory onto a file", func(t *testing.T, sfs *SwitchFS) {
			mkdir(t, sfs, "/src/item")
			writeFile(t, sfs, "/dst/item", "data")
		}, syscall.ENOTDIR},
		{"directory onto a symlink", func(t *testing.T, sfs *SwitchFS) {
			mkdir(t, sfs, "/src/item")
			mkdir(t, sfs, "/dst/real")
			link(t, sfs, "real", "/dst/item")
		}, syscall.ENOTDIR},
		{"directory onto a non-empty directory", func(t *testing.T, sfs *SwitchFS) {
			mkdir(t, sfs, "/src/item")
			writeFile(t, sfs, "/src/item/f", "new")
			mkdir(t, sfs, "/dst/item")
			writeFile(t, sfs, "/dst/item/f", "keep")
		}, syscall.ENOTEMPTY},
	}
	for _, tt := range refused {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			tt.build(t, f.sfs)
			err := f.sfs.Rename("/src/item", "/dst/item")
			le, ok := err.(*os.LinkError)
			if !ok || le.Old != "/src/item" || le.New != "/dst/item" || !errors.Is(err, tt.want) {
				t.Fatalf("Rename = %#v, want a LinkError wrapping %v", err, tt.want)
			}
			if _, err := f.sfs.Lstat("/src/item"); err != nil {
				t.Fatalf("source removed by a refused move: %v", err)
			}
			if _, err := f.sfs.Lstat("/dst/item"); err != nil {
				t.Fatalf("destination removed by a refused move: %v", err)
			}
		})
	}
	if t.Failed() {
		return
	}

	// The non-empty case left the destination's file alone.
	f := setup(t)
	refused[4].build(t, f.sfs)
	_ = f.sfs.Rename("/src/item", "/dst/item")
	if got := readFile(t, f.sfs, "/dst/item/f"); got != "keep" {
		t.Fatalf("/dst/item/f = %q", got)
	}

	faults := []struct {
		name  string
		build func(t *testing.T, sfs *SwitchFS)
		fault string
	}{
		{"destination lstat fails", buildFile, "lstat"},
		{"replacing the destination fails", func(t *testing.T, sfs *SwitchFS) {
			buildFile(t, sfs)
			writeFile(t, sfs, "/dst/item", "old")
		}, "remove"},
		{"listing the destination directory fails", func(t *testing.T, sfs *SwitchFS) {
			buildDir(t, sfs)
			mkdir(t, sfs, "/dst/item")
		}, "readdir"},
	}
	for _, tt := range faults {
		t.Run(tt.name, func(t *testing.T) {
			f := setup(t)
			tt.build(t, f.sfs)
			f.dst.fail[tt.fault] = true
			if err := f.sfs.Rename("/src/item", "/dst/item"); !errors.Is(err, errInjected) {
				t.Fatalf("Rename = %v, want the injected fault", err)
			}
			if _, err := f.src.SymlinkFileSystem.Lstat("/item"); err != nil {
				t.Fatalf("source removed after failed move: %v", err)
			}
		})
	}
}

// TestCrossBackendMove_ClosesSourceBeforeRemove uses a source that refuses to
// remove a file while any of its files is open, as some hosts do, and checks
// that the move closes every handle it opened.
func TestCrossBackendMove_ClosesSourceBeforeRemove(t *testing.T) {
	src, dst := newFaultFS(t), newFaultFS(t)
	src.busyRemove = true
	sfs := moveFixture(t, src, dst)
	buildDir(t, sfs)
	writeFile(t, sfs, "/src/single", "one")

	if err := sfs.Rename("/src/single", "/dst/single"); err != nil {
		t.Fatalf("file move = %v", err)
	}
	if err := sfs.Rename("/src/item", "/dst/item"); err != nil {
		t.Fatalf("directory move = %v", err)
	}
	if n, m := src.open.Load(), dst.open.Load(); n != 0 || m != 0 {
		t.Fatalf("open handles after moves: source %d, destination %d", n, m)
	}
	if got := readFile(t, sfs, "/dst/item/f"); got != "data" {
		t.Fatalf("/dst/item/f = %q", got)
	}
}

// dotsFS lists "." and ".." in every directory, as some backends do.
type dotsFS struct{ *faultFS }

type dotEntry string

func (d dotEntry) Name() string               { return string(d) }
func (dotEntry) IsDir() bool                  { return true }
func (dotEntry) Type() iofs.FileMode          { return iofs.ModeDir }
func (dotEntry) Info() (iofs.FileInfo, error) { return nil, errors.New("no info") }

func (d dotsFS) ReadDir(name string) ([]iofs.DirEntry, error) {
	entries, err := d.faultFS.ReadDir(name)
	if err != nil {
		return nil, err
	}
	return append([]iofs.DirEntry{dotEntry("."), dotEntry("..")}, entries...), nil
}

// TestCrossBackendMove_DotEntries: a source listing "." and ".." must not
// send the move round the same directory forever, and a destination listing
// only them counts as empty.
func TestCrossBackendMove_DotEntries(t *testing.T) {
	src, dst := dotsFS{newFaultFS(t)}, dotsFS{newFaultFS(t)}
	sfs := moveFixture(t, src, dst)
	buildDir(t, sfs)
	if err := sfs.Mkdir("/dst/item", 0755); err != nil {
		t.Fatal(err)
	}
	if err := sfs.Rename("/src/item", "/dst/item"); err != nil {
		t.Fatalf("Rename = %v", err)
	}
	if got := readFile(t, sfs, "/dst/item/f"); got != "data" {
		t.Fatalf("/dst/item/f = %q", got)
	}
	if _, err := sfs.Stat("/src/item"); !os.IsNotExist(err) {
		t.Fatalf("source left behind: %v", err)
	}
}
