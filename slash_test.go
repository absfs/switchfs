package switchfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"testing"
	"time"

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
func (d osDir) Symlink(target, n string) error            { return os.Symlink(target, d.p(n)) }
func (d osDir) Chdir(n string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	defer os.Chdir(wd)
	return os.Chdir(d.p(n))
}

// sfsDir is a SwitchFS, with names relative to a virtual directory.
type sfsDir struct {
	fs   *SwitchFS
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
	defer d.fs.Chdir("/")
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
	{"Mkdir", func(d slashFS, n string) (string, error) { return "", d.Mkdir(n, 0755) }},
	{"MkdirAll", func(d slashFS, n string) (string, error) { return "", d.MkdirAll(n, 0755) }},
	{"Remove", func(d slashFS, n string) (string, error) { return "", d.Remove(n) }},
	{"RemoveAll", func(d slashFS, n string) (string, error) { return "", d.RemoveAll(n) }},
	{"RenameFrom", func(d slashFS, n string) (string, error) { return "", d.Rename(n, "new") }},
	{"RenameFileTo", func(d slashFS, n string) (string, error) { return "", d.Rename("file", n) }},
	{"RenameDirTo", func(d slashFS, n string) (string, error) { return "", d.Rename("dir2", n) }},
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
	{"Readlink", func(d slashFS, n string) (string, error) { return d.Readlink(n) }},
	{"Symlink", func(d slashFS, n string) (string, error) { return "", d.Symlink("file", n) }},
	{"Chdir", func(d slashFS, n string) (string, error) { return "", d.Chdir(n) }},
}

// slashNames are the names each operation is called with: one of each kind
// of object with a trailing slash, a slash after a missing parent and after
// a file, and some controls without a slash.
var slashNames = []string{
	"file/", "dir/", "full/", "missing/", "lfile/", "ldir/", "ldangle/",
	"nodir/x/", "file/x/", "dir//",
	"file", "dir", "missing", "ldir", "ldangle",
}

// slashTree lists every name the fixture has or an operation might create,
// so the state after a call can be compared.
var slashTree = []string{
	"file", "dir", "dir2", "full", "full/child", "missing", "new", "nowhere",
	"lfile", "ldir", "ldangle", "nodir", "dir/x", "dir2/x",
}

// slashFixture creates the fixture through d: a file, two empty
// directories, a non-empty one, and symlinks to the file, to a directory and
// to nothing. It reports whether symlinks could be made.
func slashFixture(t *testing.T, d slashFS, create func(name, data string)) bool {
	t.Helper()
	create("file", "data")
	for _, dir := range []string{"dir", "dir2", "full"} {
		if err := d.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	create("full/child", "child")
	links := true
	for link, target := range map[string]string{"lfile": "file", "ldir": "dir", "ldangle": "nowhere"} {
		if err := d.Symlink(target, link); err != nil {
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
	return fmt.Sprintf("%T errno=%s notexist=%v exist=%v", err, errno,
		errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrExist))
}

// TestTrailingSlash_MatchesHostOS runs every operation, with every kind of
// trailing-slash name, through package os on a host directory and through
// SwitchFS onto osfs over another, and requires the same result: error
// type, errno, how the error tests against fs.ErrNotExist and fs.ErrExist,
// what was found, and what was left on disk. The expected results come
// from the host, so the test checks Linux, macOS and Windows rules on each.
func TestTrailingSlash_MatchesHostOS(t *testing.T) {
	host, err := osfs.NewFS()
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range slashOps {
		for _, name := range slashNames {
			op, name := op, name
			t.Run(op.name+"/"+name, func(t *testing.T) {
				osRoot := t.TempDir()
				od := osDir{root: osRoot}
				links := slashFixture(t, od, func(n, data string) {
					if err := os.WriteFile(filepath.Join(osRoot, filepath.FromSlash(n)), []byte(data), 0644); err != nil {
						t.Fatal(err)
					}
				})

				sfsRoot, err := filepath.Abs(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				sfs, err := New(
					WithDefault(mustMemFS(t)),
					WithRoute("/work", host, WithRewriter(ReplacePrefix("/work", osfs.FromNative(sfsRoot)))),
				)
				if err != nil {
					t.Fatal(err)
				}
				sd := sfsDir{fs: sfs, root: "/work"}
				slashFixture(t, sd, func(n, data string) { writeFile(t, sfs, sd.p(n), data) })

				if !links && strings.HasPrefix(name, "l") {
					t.Skip("host can't create symlinks")
				}

				osOut, osErr := op.run(od, name)
				sfsOut, sfsErr := op.run(sd, name)

				if got, want := errClass(sfsErr), errClass(osErr); got != want {
					t.Errorf("error: switchfs %s\n\t%v\nos %s\n\t%v", got, sfsErr, want, osErr)
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
		"MkdirAll ldangle/":  "EEXIST",
		"RemoveAll missing/": "ok", "RemoveAll nodir/x/": "ok", "RemoveAll file/x/": "ok",
	},
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
		"EISDIR": syscall.EISDIR, "EINVAL": syscall.EINVAL,
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
				if !hasTrailingSlash(name) {
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
					if want, ok := slashRuleWant[r][op.name+" "+name]; ok {
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
		if _, err := sfs.Stat("/l/"); err != errInjected {
			t.Fatalf("Stat(/l/) = %v, want the backend's error", err)
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
		if _, err := resolveLink(m, "/loop"); err != syscall.ELOOP {
			t.Fatalf("resolveLink(loop) = %v, want ELOOP", err)
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
