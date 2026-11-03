package switchfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/absfs/absfs"
	"github.com/absfs/osfs"
)

// slashFS is the part of the filesystem API the trailing-slash tests
// exercise, implemented by package os over a host directory (osDir) and by
// a SwitchFS (sfsDir), so one table drives both.
type slashFS interface {
	Stat(name string) (os.FileInfo, error)
	Lstat(name string) (os.FileInfo, error)
	OpenFile(name string, flag int, perm os.FileMode) (io.Closer, error)
	Mkdir(name string, perm os.FileMode) error
	MkdirAll(name string, perm os.FileMode) error
	Remove(name string) error
	RemoveAll(name string) error
	Rename(oldpath, newpath string) error
	Chmod(name string, mode os.FileMode) error
	Chtimes(name string, atime, mtime time.Time) error
	Chown(name string, uid, gid int) error
	Lchown(name string, uid, gid int) error
	Truncate(name string, size int64) error
	ReadDir(name string) ([]fs.DirEntry, error)
	ReadFile(name string) ([]byte, error)
	Readlink(name string) (string, error)
	Symlink(oldname, newname string) error
	Chdir(dir string) error
}

// osDir is package os, with names relative to a host directory. Names are
// joined with a slash and not cleaned, so a trailing slash reaches os.
type osDir struct{ root string }

func (d osDir) p(name string) string { return d.root + "/" + name }

func (d osDir) Stat(n string) (os.FileInfo, error)  { return os.Stat(d.p(n)) }
func (d osDir) Lstat(n string) (os.FileInfo, error) { return os.Lstat(d.p(n)) }
func (d osDir) OpenFile(n string, flag int, perm os.FileMode) (io.Closer, error) {
	f, err := os.OpenFile(d.p(n), flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}
func (d osDir) Mkdir(n string, perm os.FileMode) error    { return os.Mkdir(d.p(n), perm) }
func (d osDir) MkdirAll(n string, perm os.FileMode) error { return os.MkdirAll(d.p(n), perm) }
func (d osDir) Remove(n string) error                     { return os.Remove(d.p(n)) }
func (d osDir) RemoveAll(n string) error                  { return os.RemoveAll(d.p(n)) }
func (d osDir) Rename(o, n string) error                  { return os.Rename(d.p(o), d.p(n)) }
func (d osDir) Chmod(n string, mode os.FileMode) error    { return os.Chmod(d.p(n), mode) }
func (d osDir) Chtimes(n string, a, m time.Time) error    { return os.Chtimes(d.p(n), a, m) }
func (d osDir) Chown(n string, uid, gid int) error        { return os.Chown(d.p(n), uid, gid) }
func (d osDir) Lchown(n string, uid, gid int) error       { return os.Lchown(d.p(n), uid, gid) }
func (d osDir) Truncate(n string, size int64) error       { return os.Truncate(d.p(n), size) }
func (d osDir) ReadDir(n string) ([]fs.DirEntry, error)   { return os.ReadDir(d.p(n)) }
func (d osDir) ReadFile(n string) ([]byte, error)         { return os.ReadFile(d.p(n)) }
func (d osDir) Readlink(n string) (string, error)         { return os.Readlink(d.p(n)) }
func (d osDir) Symlink(target, n string) error {
	return os.Symlink(filepath.FromSlash(target), d.p(n))
}
func (d osDir) Chdir(n string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(wd)
	return os.Chdir(d.p(n))
}

// sfsDir is an absfs filesystem (a SwitchFS, or osfs on its own), with
// names relative to a directory in it.
type sfsDir struct {
	fs   absfs.SymlinkFileSystem
	root string
}

func (d sfsDir) p(name string) string { return d.root + "/" + name }

func (d sfsDir) Stat(n string) (os.FileInfo, error)  { return d.fs.Stat(d.p(n)) }
func (d sfsDir) Lstat(n string) (os.FileInfo, error) { return d.fs.Lstat(d.p(n)) }
func (d sfsDir) OpenFile(n string, flag int, perm os.FileMode) (io.Closer, error) {
	f, err := d.fs.OpenFile(d.p(n), flag, perm)
	if err != nil {
		return nil, err
	}
	return f, nil
}
func (d sfsDir) Mkdir(n string, perm os.FileMode) error    { return d.fs.Mkdir(d.p(n), perm) }
func (d sfsDir) MkdirAll(n string, perm os.FileMode) error { return d.fs.MkdirAll(d.p(n), perm) }
func (d sfsDir) Remove(n string) error                     { return d.fs.Remove(d.p(n)) }
func (d sfsDir) RemoveAll(n string) error                  { return d.fs.RemoveAll(d.p(n)) }
func (d sfsDir) Rename(o, n string) error                  { return d.fs.Rename(d.p(o), d.p(n)) }
func (d sfsDir) Chmod(n string, mode os.FileMode) error    { return d.fs.Chmod(d.p(n), mode) }
func (d sfsDir) Chtimes(n string, a, m time.Time) error    { return d.fs.Chtimes(d.p(n), a, m) }
func (d sfsDir) Chown(n string, uid, gid int) error        { return d.fs.Chown(d.p(n), uid, gid) }
func (d sfsDir) Lchown(n string, uid, gid int) error       { return d.fs.Lchown(d.p(n), uid, gid) }
func (d sfsDir) Truncate(n string, size int64) error       { return d.fs.Truncate(d.p(n), size) }
func (d sfsDir) ReadDir(n string) ([]fs.DirEntry, error)   { return d.fs.ReadDir(d.p(n)) }
func (d sfsDir) ReadFile(n string) ([]byte, error)         { return d.fs.ReadFile(d.p(n)) }
func (d sfsDir) Readlink(n string) (string, error)         { return d.fs.Readlink(d.p(n)) }
func (d sfsDir) Symlink(target, n string) error            { return d.fs.Symlink(target, d.p(n)) }
func (d sfsDir) Chdir(n string) error {
	wd, err := d.fs.Getwd()
	if err != nil {
		return err
	}
	defer d.fs.Chdir(wd)
	return d.fs.Chdir(d.p(n))
}

// slashOp is one call. It returns a description of any result worth
// comparing beyond the error (the type of what Stat found, say).
type slashOp struct {
	name string
	run  func(d slashFS, name string) (string, error)
}

func statResult(info os.FileInfo, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %v", info.Name(), info.Mode().Type()), nil
}

func openResult(f io.Closer, err error) (string, error) {
	if err != nil {
		return "", err
	}
	return "", f.Close()
}

var slashOps = []slashOp{
	{"Stat", func(d slashFS, n string) (string, error) { return statResult(d.Stat(n)) }},
	{"Lstat", func(d slashFS, n string) (string, error) { return statResult(d.Lstat(n)) }},
	{"Open", func(d slashFS, n string) (string, error) { return openResult(d.OpenFile(n, os.O_RDONLY, 0)) }},
	{"OpenRDWR", func(d slashFS, n string) (string, error) { return openResult(d.OpenFile(n, os.O_RDWR, 0)) }},
	{"Create", func(d slashFS, n string) (string, error) {
		return openResult(d.OpenFile(n, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644))
	}},
	{"CreateRO", func(d slashFS, n string) (string, error) {
		return openResult(d.OpenFile(n, os.O_RDONLY|os.O_CREATE, 0644))
	}},
	{"OpenNofollow", func(d slashFS, n string) (string, error) {
		return openResult(d.OpenFile(n, os.O_RDONLY|oNoFollow, 0))
	}},
	{"Mkdir", func(d slashFS, n string) (string, error) { return "", d.Mkdir(n, 0755) }},
	{"MkdirAll", func(d slashFS, n string) (string, error) { return "", d.MkdirAll(n, 0755) }},
	{"Remove", func(d slashFS, n string) (string, error) { return "", d.Remove(n) }},
	{"RemoveAll", func(d slashFS, n string) (string, error) { return "", d.RemoveAll(n) }},
	{"RenameFrom", func(d slashFS, n string) (string, error) { return "", d.Rename(n, "new") }},
	{"RenameFileTo", func(d slashFS, n string) (string, error) { return "", d.Rename("file", n) }},
	{"RenameDirTo", func(d slashFS, n string) (string, error) { return "", d.Rename("dir2", n) }},
	{"RenameDirOnto", func(d slashFS, n string) (string, error) { return "", d.Rename("dir", n) }},
	{"Chmod", func(d slashFS, n string) (string, error) { return "", d.Chmod(n, 0700) }},
	{"Chtimes", func(d slashFS, n string) (string, error) {
		return "", d.Chtimes(n, time.Unix(1e9, 0), time.Unix(1e9, 0))
	}},
	{"Chown", func(d slashFS, n string) (string, error) { return "", d.Chown(n, os.Getuid(), os.Getgid()) }},
	{"Lchown", func(d slashFS, n string) (string, error) { return "", d.Lchown(n, os.Getuid(), os.Getgid()) }},
	{"Truncate", func(d slashFS, n string) (string, error) { return "", d.Truncate(n, 0) }},
	{"ReadDir", func(d slashFS, n string) (string, error) {
		entries, err := d.ReadDir(n)
		return fmt.Sprint(len(entries)), err
	}},
	{"ReadFile", func(d slashFS, n string) (string, error) {
		data, err := d.ReadFile(n)
		return string(data), err
	}},
	{"Readlink", func(d slashFS, n string) (string, error) {
		target, err := d.Readlink(n)
		return filepath.ToSlash(target), err // osfs reports targets with slashes
	}},
	{"Symlink", func(d slashFS, n string) (string, error) { return "", d.Symlink("file", n) }},
	{"Chdir", func(d slashFS, n string) (string, error) { return "", d.Chdir(n) }},
}

// slashNames are the names each operation is called with: one of each kind
// of object with a trailing slash, a slash after a missing parent and after
// a file, and some controls without a slash. The links cover a loop, chains
// of links, and links reached through a symlinked parent whose targets
// climb out with "..", which must resolve against the directory the parent
// link leads to, as the kernel does, and not against the name.
var slashNames = []string{
	"file/", "dir/", "full/", "missing/", "lfile/", "ldir/", "ldangle/",
	"lloop/", "lchain/", "lchainf/", "pl/up/", "pl/upd/", "pl/f/", "pl/d1/", "r/sub/upd/",
	"nodir/x/", "file/x/", "dir//",
	"file", "dir", "missing", "ldir", "ldangle", "lloop",
}

// slashLinked reports whether name involves a symlink, so needs a host that
// can make them.
func slashLinked(name string) bool {
	return strings.HasPrefix(name, "l") || strings.HasPrefix(name, "pl/") || strings.HasPrefix(name, "r/")
}

// slashTree lists every name the fixture has or an operation might create,
// so the state after a call can be compared.
var slashTree = []string{
	"file", "dir", "dir2", "full", "full/child", "missing", "new", "nowhere",
	"lfile", "ldir", "ldangle", "lloop", "lchain", "lchainf", "nodir", "dir/x", "dir2/x",
	"r", "r/sub", "r/target", "r/newdir", "target", "newdir", "pl", "r/sub/up", "r/sub/upd",
	"r/sub/f", "r/sub/d1", "r/sub/nowhere1",
}

// slashLinks are the fixture's symlinks, in the order they are made (a
// chain's target first, so Windows makes directory links to directories).
var slashLinks = [][2]string{
	{"lfile", "file"}, {"ldir", "dir"}, {"ldangle", "nowhere"}, {"lloop", "lloop"},
	{"lchain", "ldir"}, {"lchainf", "lfile"},
	{"pl", "r/sub"}, {"r/sub/up", "../target"}, {"r/sub/upd", "../newdir"}, {"r/sub/d1", "nowhere1"},
}

// slashFixture creates the fixture through d: a file, empty directories, a
// non-empty one, a decoy "target" beside "r/target", and slashLinks. It
// reports whether symlinks could be made.
func slashFixture(t *testing.T, d slashFS, create func(name, data string)) bool {
	t.Helper()
	create("file", "data")
	for _, dir := range []string{"dir", "dir2", "full", "r", "r/sub", "r/target", "target"} {
		if err := d.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	create("full/child", "child")
	create("r/sub/f", "f")
	links := true
	for _, l := range slashLinks {
		if err := d.Symlink(l[1], l[0]); err != nil {
			links = false
		}
	}
	return links
}

// slashState describes what is in the fixture, for comparing the effect of
// a call.
func slashState(d slashFS) string {
	var parts []string
	for _, name := range slashTree {
		if info, err := d.Lstat(name); err == nil {
			parts = append(parts, name+":"+info.Mode().Type().String())
		}
	}
	return strings.Join(parts, " ")
}

// errClass describes an error by what callers can test: its type, its
// errno, and whether it matches fs.ErrNotExist and fs.ErrExist.
func errClass(err error) string {
	if err == nil {
		return "ok"
	}
	errno := "none"
	var en syscall.Errno
	if errors.As(err, &en) {
		errno = fmt.Sprintf("%d (%v)", uintptr(en), en)
	}
	// os on Windows names the system call (CreateFile, say) as the Op, so
	// only Unix hosts, where it names the method, compare it.
	op := "none"
	var pe *os.PathError
	var le *os.LinkError
	switch {
	case runtime.GOOS == "windows":
	case errors.As(err, &pe):
		op = pe.Op
	case errors.As(err, &le):
		op = le.Op
	}
	return fmt.Sprintf("%T op=%s errno=%s notexist=%v exist=%v", err, op, errno,
		errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrExist))
}

// TestTrailingSlash_MatchesHostOS runs every operation, with every kind of
// trailing-slash name, through package os on a host directory and through
// SwitchFS onto osfs over another, and requires the same result: error
// type, errno, how the error tests against fs.ErrNotExist and fs.ErrExist,
// what was found, and what was left on disk. The expected results come
// from the host, so the test checks Linux, macOS and Windows rules on each.
//
// The one allowance is for osfs itself: where osfs, called directly and
// without the slash, already disagrees with os, switchfs can only pass the
// backend's answer on (osfs.ReadDir on Windows reads directories with
// FindFirstFileEx and reports other errnos than os.ReadDir). Such a case is
// skipped, with both results, only when switchfs gives exactly what osfs
// gives without the slash.
func TestTrailingSlash_MatchesHostOS(t *testing.T) {
	host, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range slashOps {
		for _, name := range slashNames {
			op, name := op, name
			t.Run(op.name+"/"+name, func(t *testing.T) {
				od, links := osFixture(t)
				sd := hostFixture(t, host, true)
				if !links && slashLinked(name) {
					t.Skip("host can't create symlinks")
				}

				osOut, osErr := op.run(od, name)
				sfsOut, sfsErr := op.run(sd, name)
				gotErr, wantErr := errClass(sfsErr), errClass(osErr)

				if gotErr != wantErr {
					plain := strings.TrimRight(name, "/")
					_, backendErr := op.run(hostFixture(t, host, false), plain)
					odPlain, _ := osFixture(t)
					_, osPlainErr := op.run(odPlain, plain)
					if errClass(backendErr) == gotErr && errClass(osPlainErr) != gotErr {
						t.Skipf("osfs differs from os here: osfs %s (%v), os %s (%v) without the slash",
							gotErr, backendErr, errClass(osPlainErr), osPlainErr)
					}
					t.Errorf("error: switchfs %s\n\t%v\nos %s\n\t%v", gotErr, sfsErr, wantErr, osErr)
				}
				if sfsOut != osOut {
					t.Errorf("result: switchfs %q, os %q", sfsOut, osOut)
				}
				if got, want := slashState(sd), slashState(od); got != want {
					t.Errorf("left behind:\nswitchfs %s\nos       %s", got, want)
				}
			})
		}
	}
}

// osFixture builds the fixture in a new host directory, for package os.
func osFixture(t *testing.T) (osDir, bool) {
	t.Helper()
	root := t.TempDir()
	d := osDir{root: root}
	links := slashFixture(t, d, func(n, data string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(n)), []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	})
	return d, links
}

// hostFixture builds the fixture in a new host directory and returns it as
// /work in a SwitchFS routing /work there through osfs, or, when switched
// is false, as the directory in osfs itself.
func hostFixture(t *testing.T, host *osfs.FileSystem, switched bool) sfsDir {
	t.Helper()
	root, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := sfsDir{fs: host, root: osfs.FromNative(root)}
	if switched {
		sfs, err := New(
			WithDefault(mustMemFS(t)),
			WithRoute("/work", host, WithRewriter(ReplacePrefix("/work", d.root))),
		)
		if err != nil {
			t.Fatal(err)
		}
		d = sfsDir{fs: sfs, root: "/work"}
	}
	slashFixture(t, d, func(n, data string) { writeFile(t, d.fs, d.p(n), data) })
	return d
}

// slashSim runs op on name, with trailing-slash rules r, over a memfs
// fixture, and returns the error, the result and the fixture's state.
func slashSim(t *testing.T, r slashRules, op slashOp, name string) (error, string, string) {
	t.Helper()
	sfs, err := New(WithDefault(mustMemFS(t)))
	if err != nil {
		t.Fatal(err)
	}
	sfs.slash = r
	if err := sfs.Mkdir("/work", 0755); err != nil {
		t.Fatal(err)
	}
	d := sfsDir{fs: sfs, root: "/work"}
	slashFixture(t, d, func(n, data string) { writeFile(t, sfs, d.p(n), data) })
	out, err := op.run(d, name)
	return err, out, slashState(d)
}

// slashRuleWant lists, for each platform's rules, the calls where a
// trailing slash changes the result: the errno ("ENOTDIR" meaning the
// platform's not-a-directory errno) or "ok", then "+name" or "-name" for an
// entry the call creates or removes. Every other call must do exactly
// what the same call without the slash does. The entries are the results
// os gives on each platform, as TestTrailingSlash_MatchesHostOS confirms
// on each host. (Some, such as RemoveAll of a missing name, differ from
// memfs without the slash only because memfs differs from os there.)
var slashRuleWant = map[slashRules]map[string]string{
	slashLinux: {
		"Stat file/": "ENOTDIR", "Stat lfile/": "ENOTDIR",
		"Lstat file/": "ENOTDIR", "Lstat lfile/": "ENOTDIR", "Lstat ldangle/": "ENOENT",
		"Open file/": "ENOTDIR", "Open lfile/": "ENOTDIR",
		"OpenRDWR file/": "ENOTDIR", "OpenRDWR lfile/": "ENOTDIR",
		"Create file/": "EISDIR", "Create dir/": "EISDIR", "Create full/": "EISDIR", "Create missing/": "EISDIR",
		"Create lfile/": "EISDIR", "Create ldir/": "EISDIR", "Create ldangle/": "EISDIR",
		"CreateRO file/": "EISDIR", "CreateRO dir/": "EISDIR", "CreateRO full/": "EISDIR", "CreateRO missing/": "EISDIR",
		"CreateRO lfile/": "EISDIR", "CreateRO ldir/": "EISDIR", "CreateRO ldangle/": "EISDIR",
		"MkdirAll file/": "EEXIST", "MkdirAll lfile/": "EEXIST",
		"Remove file/": "ENOTDIR", "Remove lfile/": "ENOTDIR", "Remove ldir/": "ENOTDIR", "Remove ldangle/": "ENOTDIR",
		"RenameFrom file/": "ENOTDIR", "RenameFrom lfile/": "ENOTDIR", "RenameFrom ldir/": "ENOTDIR", "RenameFrom ldangle/": "ENOTDIR",
		"RenameFileTo file/": "ENOTDIR", "RenameFileTo lfile/": "ENOTDIR", "RenameFileTo missing/": "ENOTDIR", "RenameFileTo ldangle/": "ENOTDIR",
		"RenameDirTo file/": "ENOTDIR", "RenameDirTo lfile/": "ENOTDIR", "RenameDirTo ldangle/": "ENOTDIR",
		"Chmod file/": "ENOTDIR", "Chmod lfile/": "ENOTDIR",
		"Chtimes file/": "ENOTDIR", "Chtimes lfile/": "ENOTDIR",
		"Chown file/": "ENOTDIR", "Chown lfile/": "ENOTDIR",
		"Lchown file/": "ENOTDIR", "Lchown lfile/": "ENOTDIR", "Lchown ldangle/": "ENOENT",
		"Truncate file/": "ENOTDIR", "Truncate lfile/": "ENOTDIR",
		"ReadDir file/": "ENOTDIR", "ReadDir lfile/": "ENOTDIR",
		"ReadFile file/": "ENOTDIR", "ReadFile lfile/": "ENOTDIR",
		"Readlink file/": "ENOTDIR", "Readlink lfile/": "ENOTDIR", "Readlink dir/": "EINVAL", "Readlink full/": "EINVAL",
		"Readlink ldir/": "EINVAL", "Readlink missing/": "ENOENT", "Readlink ldangle/": "ENOENT",
		"Symlink missing/": "ENOENT",
		"Chdir file/":      "ENOTDIR", "Chdir lfile/": "ENOTDIR",
		"Create dir//": "EISDIR", "CreateRO dir//": "EISDIR", "MkdirAll ldangle/": "EEXIST",
		"RenameFileTo ldir/": "EEXIST", "RenameDirTo ldir/": "EEXIST",
		"Readlink dir//":     "EINVAL",
		"RemoveAll missing/": "ok", "RemoveAll nodir/x/": "ok", "RemoveAll file/x/": "ok",
		"Create lloop/": "EISDIR", "CreateRO lloop/": "EISDIR", "Lstat lloop/": "ELOOP", "MkdirAll lloop/": "EEXIST",
		"Readlink lloop/": "ELOOP", "Remove lloop/": "ENOTDIR", "RenameFrom lloop/": "ENOTDIR",
		"RenameFileTo lloop/": "ENOTDIR", "RenameDirTo lloop/": "ENOTDIR",
		"RenameDirOnto dir/": "ok", "RenameDirOnto dir//": "ok", "RenameDirOnto file/": "ENOTDIR",
		"RenameDirOnto lfile/": "ENOTDIR", "RenameDirOnto ldir/": "ENOTDIR", "RenameDirOnto ldangle/": "ENOTDIR",
		"RenameDirOnto lloop/": "ENOTDIR",
	},
	slashDarwin: {
		"Stat file/": "ENOTDIR", "Stat lfile/": "ENOTDIR",
		"Lstat file/": "ENOTDIR", "Lstat lfile/": "ENOTDIR", "Lstat ldangle/": "ENOENT",
		"Open file/": "ENOTDIR", "Open lfile/": "ENOTDIR",
		"OpenRDWR file/": "ENOTDIR", "OpenRDWR lfile/": "ENOTDIR",
		"Create file/": "ENOTDIR", "Create lfile/": "ENOTDIR", "Create missing/": "ENOENT", "Create ldangle/": "ENOENT",
		"CreateRO file/": "ENOTDIR", "CreateRO lfile/": "ENOTDIR", "CreateRO missing/": "ENOENT", "CreateRO ldangle/": "ENOENT",
		"Mkdir file/": "ENOTDIR", "Mkdir lfile/": "ENOTDIR", "Mkdir ldangle/": "ok +nowhere",
		"MkdirAll file/": "ENOTDIR", "MkdirAll lfile/": "ENOTDIR", "MkdirAll ldangle/": "ok +nowhere",
		"Remove file/": "ENOTDIR", "Remove lfile/": "ENOTDIR", "Remove ldir/": "ok -dir", "Remove ldangle/": "ENOENT",
		"RemoveAll ldir/": "ok -dir", "RemoveAll ldangle/": "ok",
		"RenameFrom file/": "ENOTDIR", "RenameFrom lfile/": "ENOTDIR", "RenameFrom ldir/": "ok -dir +new", "RenameFrom ldangle/": "ENOENT",
		"RenameFileTo file/": "ENOTDIR", "RenameFileTo lfile/": "ENOTDIR", "RenameFileTo missing/": "ENOENT", "RenameFileTo ldangle/": "ENOENT",
		"RenameDirTo file/": "ENOTDIR", "RenameDirTo lfile/": "ENOTDIR", "RenameDirTo ldangle/": "ok -dir2 +nowhere",
		"Chmod file/": "ENOTDIR", "Chmod lfile/": "ENOTDIR",
		"Chtimes file/": "ENOTDIR", "Chtimes lfile/": "ENOTDIR",
		"Chown file/": "ENOTDIR", "Chown lfile/": "ENOTDIR",
		"Lchown file/": "ENOTDIR", "Lchown lfile/": "ENOTDIR", "Lchown ldangle/": "ENOENT",
		"Truncate file/": "ENOTDIR", "Truncate lfile/": "ENOTDIR",
		"ReadDir file/": "ENOTDIR", "ReadDir lfile/": "ENOTDIR",
		"ReadFile file/": "ENOTDIR", "ReadFile lfile/": "ENOTDIR",
		"Readlink file/": "ENOTDIR", "Readlink lfile/": "ENOTDIR", "Readlink dir/": "EINVAL", "Readlink full/": "EINVAL",
		"Readlink ldir/": "EINVAL", "Readlink missing/": "ENOENT", "Readlink ldangle/": "ENOENT",
		"Symlink file/": "ENOTDIR", "Symlink lfile/": "ENOTDIR", "Symlink missing/": "ENOENT", "Symlink ldangle/": "ENOENT",
		"Chdir file/": "ENOTDIR", "Chdir lfile/": "ENOTDIR",
		"RenameFileTo ldir/": "EEXIST", "RenameDirTo ldir/": "EEXIST",
		"Readlink dir//":     "EINVAL",
		"RemoveAll missing/": "ok", "RemoveAll nodir/x/": "ok", "RemoveAll file/x/": "ok",
		"Lstat lloop/": "ELOOP", "Mkdir lloop/": "ELOOP", "MkdirAll lloop/": "ELOOP", "Readlink lloop/": "ELOOP",
		"Remove lloop/": "ELOOP", "RenameFrom lloop/": "ELOOP", "RenameFileTo lloop/": "ELOOP",
		"RenameDirTo lloop/": "ELOOP", "Symlink lloop/": "ELOOP",
		"RenameDirOnto dir/": "ok", "RenameDirOnto dir//": "ok", "RenameDirOnto file/": "ENOTDIR",
		"RenameDirOnto lfile/": "ENOTDIR", "RenameDirOnto ldir/": "ok", "RenameDirOnto ldangle/": "ok -dir +nowhere",
		"RenameDirOnto lloop/": "ELOOP",
	},
	slashWindows: {
		"Stat file/": "ENOTDIR", "Stat lfile/": "ENOTDIR", "Stat ldangle/": "ENOTDIR",
		"Lstat file/": "ENOTDIR", "Lstat lfile/": "ENOTDIR", "Lstat ldangle/": "ENOTDIR",
		"Open file/": "ENOTDIR", "Open lfile/": "ENOTDIR", "Open ldangle/": "ENOTDIR",
		"OpenRDWR file/": "ENOTDIR", "OpenRDWR lfile/": "ENOTDIR", "OpenRDWR ldangle/": "ENOTDIR",
		"Create file/": "ENOTDIR", "Create lfile/": "ENOTDIR", "Create missing/": "ENOTDIR", "Create ldangle/": "ENOTDIR",
		"CreateRO file/": "ENOTDIR", "CreateRO lfile/": "ENOTDIR", "CreateRO missing/": "ENOTDIR", "CreateRO ldangle/": "ENOTDIR",
		"MkdirAll file/": "EEXIST", "MkdirAll lfile/": "EEXIST",
		"Remove file/": "ENOTDIR", "Remove lfile/": "ENOTDIR", "Remove ldangle/": "ENOTDIR",
		"RemoveAll file/": "ENOTDIR", "RemoveAll lfile/": "ENOTDIR", "RemoveAll ldangle/": "ENOTDIR",
		"RenameFrom file/": "ENOTDIR", "RenameFrom lfile/": "ENOTDIR", "RenameFrom ldangle/": "ENOTDIR",
		"Chmod file/": "ENOTDIR", "Chmod lfile/": "ENOTDIR", "Chmod ldangle/": "ENOTDIR",
		"Chtimes file/": "ENOTDIR", "Chtimes lfile/": "ENOTDIR", "Chtimes ldangle/": "ENOTDIR",
		"Truncate file/": "ENOTDIR", "Truncate lfile/": "ENOTDIR", "Truncate ldangle/": "ENOTDIR",
		"ReadDir file/": "ENOTDIR", "ReadDir lfile/": "ENOTDIR", "ReadDir ldangle/": "ENOTDIR",
		"ReadFile file/": "ENOTDIR", "ReadFile lfile/": "ENOTDIR", "ReadFile ldangle/": "ENOTDIR",
		"Readlink file/": "ENOTDIR", "Readlink lfile/": "ENOTDIR", "Readlink ldangle/": "ENOTDIR",
		"Symlink file/": "ENOTDIR", "Symlink lfile/": "ENOTDIR", "Symlink missing/": "ENOTDIR", "Symlink ldangle/": "ENOTDIR",
		"Chdir file/": "ENOTDIR", "Chdir lfile/": "ENOTDIR", "Chdir ldangle/": "ENOTDIR",
		"MkdirAll ldangle/": "EEXIST", "RenameFileTo file/x/": "ENOTDIR", "RenameDirTo file/x/": "ENOTDIR",
		"RemoveAll missing/": "ok", "RemoveAll nodir/x/": "ok", "RemoveAll file/x/": "ok",
		"Stat lloop/": "ENOTDIR", "Lstat lloop/": "ENOTDIR", "Open lloop/": "ENOTDIR", "OpenRDWR lloop/": "ENOTDIR",
		"Create lloop/": "ENOTDIR", "CreateRO lloop/": "ENOTDIR", "MkdirAll lloop/": "EEXIST",
		"Remove lloop/": "ENOTDIR", "RemoveAll lloop/": "ENOTDIR", "RenameFrom lloop/": "ENOTDIR",
		"Chmod lloop/": "ENOTDIR", "Chtimes lloop/": "ENOTDIR", "Truncate lloop/": "ENOTDIR",
		"ReadDir lloop/": "ENOTDIR", "ReadFile lloop/": "ENOTDIR", "Readlink lloop/": "ENOTDIR",
		"Symlink lloop/": "ENOTDIR", "RenameDirOnto file/x/": "ENOTDIR",
	},
}

// slashRuleAlias names, for an operation or name, the one whose
// slashRuleWant entries it shares: O_NOFOLLOW changes nothing a trailing
// slash does (the slash overrides it), a chain of links behaves as the last
// link in it, and r/sub/upd, a dangling link with a ".." target, as
// ldangle, but with its own target (r/newdir for nowhere).
var slashRuleAlias = map[string]string{
	"OpenNofollow": "Open", "lchain/": "ldir/", "lchainf/": "lfile/", "r/sub/upd/": "ldangle/",
}

// slashAliasTarget maps an aliased name's link target to its own.
var slashAliasTarget = map[string][2]string{"r/sub/upd/": {"nowhere", "r/newdir"}}

// slashRuleLookup returns the slashRuleWant entry for op on name under r,
// looking through slashRuleAlias when there is none of its own.
func slashRuleLookup(r slashRules, op, name string) (string, bool) {
	if want, ok := slashRuleWant[r][op+" "+name]; ok {
		return want, true
	}
	if alias, ok := slashRuleAlias[op]; ok {
		op = alias
	}
	target := slashAliasTarget[name]
	if alias, ok := slashRuleAlias[name]; ok {
		name = alias
	}
	want, ok := slashRuleWant[r][op+" "+name]
	if target[0] != "" {
		want = strings.ReplaceAll(want, target[0], target[1])
	}
	return want, ok
}

// stateDelta describes how after differs from before as "+name" and
// "-name" entries.
func stateDelta(before, after string) string {
	b := map[string]bool{}
	for _, e := range strings.Fields(before) {
		b[strings.SplitN(e, ":", 2)[0]] = true
	}
	a := map[string]bool{}
	for _, e := range strings.Fields(after) {
		a[strings.SplitN(e, ":", 2)[0]] = true
	}
	var d []string
	for n := range b {
		if !a[n] {
			d = append(d, "-"+n)
		}
	}
	for n := range a {
		if !b[n] {
			d = append(d, "+"+n)
		}
	}
	sort.Slice(d, func(i, j int) bool { return d[i][1:] < d[j][1:] })
	return strings.Join(d, " ")
}

// errnoName names the errnos the rules produce.
func errnoName(r slashRules, err error) string {
	if err == nil {
		return "ok"
	}
	notDir := syscall.ENOTDIR
	if r == slashWindows {
		notDir = errDirectory
	}
	for name, errno := range map[string]error{
		"ENOTDIR": notDir, "ENOENT": syscall.ENOENT, "EEXIST": syscall.EEXIST,
		"EISDIR": syscall.EISDIR, "EINVAL": syscall.EINVAL, "ELOOP": syscall.ELOOP,
	} {
		if errors.Is(err, errno) {
			return name
		}
	}
	// Some other error, from the backend: describe it without the path,
	// which differs with and without the slash.
	switch e := err.(type) {
	case *os.PathError:
		return e.Err.Error()
	case *os.LinkError:
		return e.Err.Error()
	}
	return err.Error()
}

// TestTrailingSlash_PlatformRules runs every operation under each
// platform's rules over memfs, so every host tests the rules of the others.
// Calls listed in slashRuleWant must give the listed result; the rest must
// match the same call without the trailing slash.
func TestTrailingSlash_PlatformRules(t *testing.T) {
	rules := map[slashRules]string{slashLinux: "linux", slashDarwin: "darwin", slashWindows: "windows"}
	for r, rname := range rules {
		for _, op := range slashOps {
			for _, name := range slashNames {
				if !hasTrailingSlash(name) || strings.HasPrefix(name, "pl/") {
					// memfs doesn't follow a symlink in a directory
					// component, so the symlinked parent can't be simulated;
					// TestTrailingSlash_MatchesHostOS covers it on each host.
					continue
				}
				r, op, name := r, op, name
				t.Run(rname+"/"+op.name+"/"+name, func(t *testing.T) {
					_, _, before := slashSim(t, r, slashOp{"none", func(slashFS, string) (string, error) { return "", nil }}, "")
					err, _, after := slashSim(t, r, op, name)
					got := errnoName(r, err)
					if delta := stateDelta(before, after); delta != "" {
						got += " " + delta
					}
					if want, ok := slashRuleLookup(r, op.name, name); ok {
						if got != want {
							t.Errorf("got %s (%v), want %s", got, err, want)
						}
						return
					}
					plainErr, _, plainAfter := slashSim(t, r, op, strings.TrimRight(name, "/"))
					want := errnoName(r, plainErr)
					if delta := stateDelta(before, plainAfter); delta != "" {
						want += " " + delta
					}
					if got != want {
						t.Errorf("got %s (%v); without the slash %s (%v)", got, err, want, plainErr)
					}
				})
			}
		}
	}
}

// TestTrailingSlash_WindowsNotDir checks the property that matters to
// callers on Windows: the not-a-directory error is ERROR_DIRECTORY, as os
// returns there, and so does not match fs.ErrNotExist. (syscall.ENOTDIR
// on Windows is ERROR_PATH_NOT_FOUND, which would.)
func TestTrailingSlash_WindowsNotDir(t *testing.T) {
	sfs, err := New(WithDefault(mustMemFS(t)))
	if err != nil {
		t.Fatal(err)
	}
	sfs.slash = slashWindows
	writeFile(t, sfs, "/f", "x")
	_, err = sfs.Stat("/f/")
	var pe *os.PathError
	if !errors.As(err, &pe) || pe.Err != errDirectory || pe.Path != "/f/" || pe.Op != "stat" {
		t.Fatalf("Stat(/f/) = %#v, want *PathError{stat /f/ ERROR_DIRECTORY}", err)
	}
	if errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err) {
		t.Fatalf("Stat(/f/) = %v matches not-exist", err)
	}
}

// TestTrailingSlash_HostRules checks the rules chosen for each GOOS.
func TestTrailingSlash_HostRules(t *testing.T) {
	for goos, want := range map[string]slashRules{
		"linux": slashLinux, "freebsd": slashLinux, "darwin": slashDarwin, "ios": slashDarwin, "windows": slashWindows,
	} {
		if got := hostSlashRules(goos); got != want {
			t.Errorf("hostSlashRules(%q) = %v, want %v", goos, got, want)
		}
	}
	sfs, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if sfs.slash != hostSlash {
		t.Errorf("New uses rules %v, want the host's %v", sfs.slash, hostSlash)
	}
}

// TestTrailingSlash_Results checks what succeeding calls return: Lstat
// through a slash describes the directory a link resolves to, under the
// caller's name, and Mkdir, MkdirAll and Rename accept a trailing slash on
// a new directory.
func TestTrailingSlash_Results(t *testing.T) {
	for _, r := range []slashRules{slashLinux, slashDarwin, slashWindows} {
		sfs, err := New(WithDefault(mustMemFS(t)))
		if err != nil {
			t.Fatal(err)
		}
		sfs.slash = r
		if err := sfs.Mkdir("/d/", 0755); err != nil {
			t.Fatalf("Mkdir(/d/): %v", err)
		}
		if err := sfs.MkdirAll("/a/b/c/", 0755); err != nil {
			t.Fatalf("MkdirAll(/a/b/c/): %v", err)
		}
		if err := sfs.MkdirAll("/a/b/c/", 0755); err != nil {
			t.Fatalf("MkdirAll(/a/b/c/) again: %v", err)
		}
		if err := sfs.Symlink("d", "/l"); err != nil {
			t.Fatal(err)
		}
		info, err := sfs.Lstat("/l/")
		if err != nil || !info.IsDir() || info.Name() != "l" {
			t.Fatalf("Lstat(/l/) = %v, %v; want directory named l", info, err)
		}
		if err := sfs.Rename("/a/", "/z/"); err != nil {
			t.Fatalf("Rename(/a/, /z/): %v", err)
		}
		if _, err := sfs.Stat(path.Join("/z/b/c") + "/"); err != nil {
			t.Fatalf("Stat(/z/b/c/): %v", err)
		}
		if err := sfs.Chdir("/z/b/"); err != nil {
			t.Fatalf("Chdir(/z/b/): %v", err)
		}
		if wd, _ := sfs.Getwd(); wd != "/z/b" {
			t.Fatalf("Getwd = %q after Chdir(/z/b/)", wd)
		}
	}
}

// TestTrailingSlash_BackendFailures covers the slash rules when the backend
// misbehaves: a failing Stat leaves the call to the backend, a failing
// Readlink while following a symlink is reported under the caller's name,
// and a symlink loop fails with ELOOP.
func TestTrailingSlash_BackendFailures(t *testing.T) {
	t.Run("stat fails on a symlink", func(t *testing.T) {
		ff := newFaultFS(t)
		sfs, err := New(WithDefault(ff))
		if err != nil {
			t.Fatal(err)
		}
		if err := sfs.Symlink("nowhere", "/l"); err != nil {
			t.Fatal(err)
		}
		ff.fail["stat"] = true
		sfs.slash = slashLinux // Windows reports any unfollowable link as not a directory
		if _, err := sfs.Stat("/l/"); err != errInjected {
			t.Fatalf("Stat(/l/) = %v, want the backend's error", err)
		}
		// macOS, following the link to remove what it names, fails as
		// following it did, under the caller's op and name.
		sfs.slash = slashDarwin
		err = sfs.Remove("/l/")
		var pe *os.PathError
		if !errors.As(err, &pe) || pe.Op != "remove" || pe.Path != "/l/" || pe.Err != errInjected {
			t.Fatalf("Remove(/l/) = %#v, want *PathError{remove /l/ injected}", err)
		}
	})

	t.Run("readlink fails while following", func(t *testing.T) {
		for _, op := range []struct {
			name string
			call func(*SwitchFS) error
		}{
			{"mkdir", func(s *SwitchFS) error { return s.Mkdir("/l/", 0755) }},
			{"remove", func(s *SwitchFS) error { return s.Remove("/ld/") }},
		} {
			ff := newFaultFS(t)
			sfs, err := New(WithDefault(ff))
			if err != nil {
				t.Fatal(err)
			}
			sfs.slash = slashDarwin
			if err := sfs.Mkdir("/d", 0755); err != nil {
				t.Fatal(err)
			}
			if err := sfs.Symlink("nowhere", "/l"); err != nil {
				t.Fatal(err)
			}
			if err := sfs.Symlink("d", "/ld"); err != nil {
				t.Fatal(err)
			}
			ff.fail["readlink"] = true
			err = op.call(sfs)
			var pe *os.PathError
			if !errors.As(err, &pe) || pe.Err != errInjected || !strings.HasSuffix(pe.Path, "/") {
				t.Fatalf("%s = %#v, want the readlink failure under the caller's name", op.name, err)
			}
		}
	})

	t.Run("symlink loop", func(t *testing.T) {
		m := mustMemFS(t)
		if err := m.Symlink("/loop", "/loop"); err != nil {
			t.Fatal(err)
		}
		if _, err := realPath(m, "/loop", true); err != syscall.ELOOP {
			t.Fatalf("realPath(loop) = %v, want ELOOP", err)
		}
	})

	t.Run("realPath", func(t *testing.T) {
		m := mustMemFS(t)
		for _, d := range []string{"/r", "/r/sub", "/r/target", "/target"} {
			if err := m.Mkdir(d, 0755); err != nil {
				t.Fatal(err)
			}
		}
		writeFile(t, m, "/f", "x")
		for link, target := range map[string]string{
			"/a": "r/sub", "/r/sub/up": "../target", "/abs": "/r/target",
			"/dots": "./r/../r/./target", "/via": "nope/../target", "/r/sub/new": "../newdir",
		} {
			if err := m.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
		}
		for _, c := range []struct {
			p      string
			follow bool
			want   string
			err    error
		}{
			// ".." in a target climbs out of where the parent link leads
			// (r/sub), not out of the name (/a), which would give /target.
			{"/a/up", true, "/r/target", nil},
			{"/a/up", false, "/r/sub/up", nil},
			{"/a/new", true, "/r/newdir", nil},
			{"/abs", true, "/r/target", nil},
			{"/dots", true, "/r/target", nil},
			{"/r/sub/..", false, "/r", nil},
			{"/..", false, "/", nil},
			{"r/sub", false, "r/sub", nil},
			{"/f/x", true, "", syscall.ENOTDIR},
			{"/nope/x", true, "", fs.ErrNotExist},
			{"/via", true, "", fs.ErrNotExist},
		} {
			got, err := realPath(m, c.p, c.follow)
			if got != c.want || (c.err == nil) != (err == nil) || (c.err != nil && !errors.Is(err, c.err)) {
				t.Errorf("realPath(%q, %v) = %q, %v; want %q, %v", c.p, c.follow, got, err, c.want, c.err)
			}
		}
		if sameEntry(m, "/f/x", "/r") {
			t.Error("sameEntry with an unresolvable source = true")
		}

		ff := newFaultFS(t)
		if err := ff.Mkdir("/d", 0755); err != nil {
			t.Fatal(err)
		}
		if err := ff.Symlink("d", "/l"); err != nil {
			t.Fatal(err)
		}
		ff.fail["readlink"] = true
		if _, err := realPath(ff, "/l/x", true); !errors.Is(err, errInjected) {
			t.Errorf("realPath through a failing Readlink = %v, want the backend's error", err)
		}
	})

	t.Run("windows ignores a slash through a symlinked directory", func(t *testing.T) {
		m := mustMemFS(t)
		if err := m.MkdirAll("/r/sub", 0755); err != nil {
			t.Fatal(err)
		}
		if err := m.Symlink("r/sub", "/pl"); err != nil {
			t.Fatal(err)
		}
		sfs, err := New(WithDefault(m))
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range []struct {
			r    slashRules
			name string
			want bool
		}{
			{slashWindows, "/pl/f/", false},
			{slashWindows, "/r/sub/f/", true},
			{slashWindows, "/r/sub/f", false},
			{slashLinux, "/pl/f/", true},
			{slashDarwin, "/pl/f/", true},
		} {
			sfs.slash = c.r
			if got := sfs.slashed(c.name, m, path.Clean(c.name)); got != c.want {
				t.Errorf("rules %v: slashed(%q) = %v, want %v", c.r, c.name, got, c.want)
			}
		}
	})

	t.Run("unwrapErrno", func(t *testing.T) {
		le := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: syscall.EXDEV}
		if got := unwrapErrno(le); got != syscall.EXDEV {
			t.Errorf("unwrapErrno(LinkError) = %v", got)
		}
		if got := unwrapErrno(errInjected); got != errInjected {
			t.Errorf("unwrapErrno(bare) = %v", got)
		}
	})

	t.Run("rename onto itself", func(t *testing.T) {
		// As os.Rename on Unix: the same directory under another spelling
		// is left alone; the identical name is refused as existing.
		for _, r := range []slashRules{slashLinux, slashDarwin} {
			sfs, err := New(WithDefault(mustMemFS(t)))
			if err != nil {
				t.Fatal(err)
			}
			sfs.slash = r
			if err := sfs.Mkdir("/d", 0755); err != nil {
				t.Fatal(err)
			}
			if err := sfs.Rename("/d", "/d/"); err != nil {
				t.Fatalf("Rename(/d, /d/) = %v", err)
			}
			if err := sfs.Rename("/d/", "/d/"); !errors.Is(err, syscall.EEXIST) {
				t.Fatalf("Rename(/d/, /d/) = %v, want EEXIST", err)
			}
			if _, err := sfs.Stat("/d"); err != nil {
				t.Fatalf("/d after renames onto itself: %v", err)
			}
		}
	})

	t.Run("rename of a missing source", func(t *testing.T) {
		for _, r := range []slashRules{slashLinux, slashDarwin} {
			sfs, err := New(WithDefault(mustMemFS(t)))
			if err != nil {
				t.Fatal(err)
			}
			sfs.slash = r
			if err := sfs.Rename("/missing", "/new/"); !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("Rename(missing, new/) = %v, want not-exist", err)
			}
		}
	})
}
