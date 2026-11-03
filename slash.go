package switchfs

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"runtime"
	"strings"
	"syscall"

	"github.com/absfs/absfs"
)

// Trailing slashes.
//
// Routing cleans every path, which drops a trailing slash, and backends
// can't be handed one (memfs.Mkdir("/e/") fails). So SwitchFS remembers that
// the caller's name ended in a slash and applies the rule package os gets
// from the host: such a name must resolve to a directory, following a final
// symlink. Hosts disagree on the details (which errno, and what happens to a
// symlink with a trailing slash), so the rules below are selected by
// runtime.GOOS and were taken from os on each platform; the tests compare
// them with os on the host they run on.

// slashRules names a platform's trailing-slash behaviour.
type slashRules int

const (
	slashLinux   slashRules = iota // Linux, and other Unix hosts
	slashDarwin                    // macOS and iOS
	slashWindows                   // Windows
)

// hostSlashRules returns the rules package os follows on goos.
func hostSlashRules(goos string) slashRules {
	switch goos {
	case "darwin", "ios":
		return slashDarwin
	case "windows":
		return slashWindows
	}
	return slashLinux
}

// errDirectory is Windows' ERROR_DIRECTORY ("The directory name is
// invalid."), which os returns there for a trailing slash on a
// non-directory. syscall.ENOTDIR can't stand in for it: on Windows that is
// ERROR_PATH_NOT_FOUND, which matches fs.ErrNotExist.
const errDirectory = syscall.Errno(267)

// symlinkLoopLimit bounds how many symlinks realPath follows, as the
// kernel's own limit does (40 on Linux, 32 on macOS).
const symlinkLoopLimit = 40

// errRenameSelf is renameDest's report that a rename names the same
// directory twice, which rename(2) accepts and does nothing for.
var errRenameSelf = errors.New("switchfs: rename onto itself")

// hasTrailingSlash reports whether name ends in a slash, which requires the
// object it names to be a directory.
func hasTrailingSlash(name string) bool {
	return strings.HasSuffix(name, "/")
}

// unwrapErrno returns the error inside a backend's *os.PathError or
// *os.LinkError, so it can be reported under the caller's op and name.
func unwrapErrno(err error) error {
	switch e := err.(type) {
	case *os.PathError:
		return e.Err
	case *os.LinkError:
		return e.Err
	}
	return err
}

// slashError reports err, from applying a trailing slash to name, as an
// *os.PathError for op carrying the caller's name.
func slashError(op, name string, err error) error {
	return &os.PathError{Op: op, Path: name, Err: unwrapErrno(err)}
}

// withOp reports a backend *os.PathError under op, the operation the caller
// asked for, when the slash made SwitchFS call a different one (Stat for
// Lstat, Chown for Lchown).
func withOp(err error, op string) error {
	if pe, ok := err.(*os.PathError); ok && pe.Op != op {
		return &os.PathError{Op: op, Path: pe.Path, Err: pe.Err}
	}
	return err
}

// notDir returns the platform's not-a-directory errno.
func (fs *SwitchFS) notDir() error {
	if fs.slash == slashWindows {
		return errDirectory
	}
	return syscall.ENOTDIR
}

// slashKind is what a backend path names, looked at the way a trailing
// slash needs: whether it is there, a directory, or a symlink, and what the
// symlink resolves to.
type slashKind int

const (
	kindUnknown      slashKind = iota // can't tell; the operation decides
	kindMissing                       // absent, in an existing directory
	kindDir                           // a directory
	kindFile                          // not a directory and not a symlink
	kindLinkDir                       // a symlink to a directory
	kindLinkFile                      // a symlink to a non-directory
	kindLinkDangling                  // a symlink to nothing
	kindLinkErr                       // a symlink that can't be followed (a loop, say)
)

// classify reports what p names in b. A missing name whose parent is not an
// existing directory is kindUnknown, so the backend reports the problem
// with the parent as it would without the slash. For kindLinkErr it also
// returns the errno following the link failed with (ELOOP for a loop),
// which is what a trailing slash, by following the link, runs into.
func classify(b absfs.FileSystem, p string) (slashKind, error) {
	info, err := lstat(b, p)
	if errors.Is(err, fs.ErrNotExist) {
		if parent, err := b.Stat(path.Dir(p)); err == nil && parent.IsDir() {
			return kindMissing, nil
		}
		return kindUnknown, nil
	}
	if err != nil {
		return kindUnknown, nil
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if info.IsDir() {
			return kindDir, nil
		}
		return kindFile, nil
	}
	info, err = b.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return kindLinkDangling, nil
	case err != nil:
		return kindLinkErr, unwrapErrno(err)
	case info.IsDir():
		return kindLinkDir, nil
	}
	return kindLinkFile, nil
}

// lookupNotDir reports whether looking up a name with a trailing slash
// (stat, open, chmod and the like) fails as not a directory. A dangling
// symlink is not-exist on Unix, where the link is followed, and
// not-a-directory on Windows, where it is not; so is a symlink that can't
// be followed.
func (fs *SwitchFS) lookupNotDir(k slashKind) bool {
	switch k {
	case kindFile, kindLinkFile:
		return true
	case kindLinkDangling, kindLinkErr:
		return fs.slash == slashWindows
	}
	return false
}

// checkSlash returns an *os.PathError for op when name has a trailing slash
// and p, in b, exists and is not a directory. It returns nil otherwise, and
// the operation goes ahead on p.
func (fs *SwitchFS) checkSlash(op, name string, b absfs.FileSystem, p string) error {
	if !hasTrailingSlash(name) {
		return nil
	}
	if k, _ := classify(b, p); !fs.lookupNotDir(k) {
		return nil
	}
	return &os.PathError{Op: op, Path: name, Err: fs.notDir()}
}

// realPath resolves p in b as the kernel looks a name up: a symlink in any
// directory component is followed, and so is a final one when follow is
// set. A relative link target is resolved against the directory the link
// was found in after that directory has itself been resolved, so ".." in a
// target climbs out of where a symlinked parent leads, not out of the name.
// A missing final component is kept (it is what a dangling link names); a
// missing or non-directory component before it fails with ENOENT or
// ENOTDIR, and more than symlinkLoopLimit links with ELOOP.
func realPath(b absfs.FileSystem, p string, follow bool) (string, error) {
	resolved := "/"
	if !path.IsAbs(p) {
		resolved = "."
	}
	rest := strings.Split(p, "/")
	for links := 0; len(rest) > 0; {
		elem := rest[0]
		rest = rest[1:]
		switch elem {
		case "", ".":
			continue
		case "..":
			resolved = path.Dir(resolved)
			continue
		}
		next := path.Join(resolved, elem)
		last := len(rest) == 0
		info, err := lstat(b, next)
		switch {
		case err != nil && last && errors.Is(err, fs.ErrNotExist):
			return next, nil
		case err != nil:
			return "", err
		case info.Mode()&os.ModeSymlink != 0 && (follow || !last):
			if links++; links > symlinkLoopLimit {
				return "", syscall.ELOOP
			}
			target, err := b.(absfs.SymLinker).Readlink(next) // lstat only reports symlinks from a SymLinker
			if err != nil {
				return "", err
			}
			if path.IsAbs(target) {
				resolved = "/"
			}
			rest = append(strings.Split(target, "/"), rest...)
			continue
		case !last && !info.IsDir():
			return "", syscall.ENOTDIR
		}
		resolved = next
	}
	return resolved, nil
}

// sameEntry reports whether dst, followed through a final symlink, is the
// directory entry src names (not followed), both in the one backend b.
func sameEntry(b absfs.FileSystem, src, dst string) bool {
	s, err := realPath(b, src, false)
	if err != nil {
		return false
	}
	d, err := realPath(b, dst, true)
	return err == nil && s == d
}

// openSlash applies a trailing slash to OpenFile, returning the flags to
// open p with or the errno to fail with. With O_CREATE, Linux refuses any
// such name with EISDIR before looking at it; macOS reports a missing name
// as ENOENT and Windows as not-a-directory. The slash requires a final
// symlink to be followed, so it overrides O_NOFOLLOW.
func (fs *SwitchFS) openSlash(b absfs.FileSystem, p string, flag int) (int, error) {
	k, _ := classify(b, p)
	creating := flag&os.O_CREATE != 0
	switch {
	case creating && fs.slash == slashLinux && k != kindUnknown:
		return flag, syscall.EISDIR
	case fs.lookupNotDir(k):
		return flag, fs.notDir()
	case creating && (k == kindMissing || k == kindLinkDangling):
		if fs.slash == slashWindows {
			return flag, fs.notDir()
		}
		return flag, syscall.ENOENT
	case k == kindLinkDir || k == kindLinkDangling || k == kindLinkErr:
		return flag &^ oNoFollow, nil
	}
	return flag, nil
}

// mkdir creates p in b for Mkdir(name). On macOS a trailing slash makes
// mkdir fail on a non-directory with ENOTDIR, on a link that can't be
// followed with the reason (ELOOP), and create the target of a dangling
// symlink; elsewhere the backend's own error (EEXIST) stands.
func (fs *SwitchFS) mkdir(name string, b absfs.FileSystem, p string, perm os.FileMode) error {
	if hasTrailingSlash(name) && fs.slash == slashDarwin {
		switch k, err := classify(b, p); k {
		case kindFile, kindLinkFile:
			return &os.PathError{Op: "mkdir", Path: name, Err: fs.notDir()}
		case kindLinkErr:
			return &os.PathError{Op: "mkdir", Path: name, Err: err}
		case kindLinkDangling:
			target, err := realPath(b, p, true)
			if err != nil {
				return slashError("mkdir", name, err)
			}
			p = target
		}
	}
	return pathError(b.Mkdir(p, perm), name)
}

// removalPath returns the path that Remove and the source of Rename act on
// for name, or the errno they fail with. A trailing slash on a
// non-directory fails as not a directory. On a symlink to a directory Linux
// fails with ENOTDIR, macOS acts on the directory the link resolves to, and
// Windows on the link itself. A dangling symlink, or one that can't be
// followed, is not-a-directory except on macOS, which follows it to ENOENT
// or to whatever stopped it (ELOOP).
func (fs *SwitchFS) removalPath(name string, b absfs.FileSystem, p string) (string, error) {
	if !hasTrailingSlash(name) {
		return p, nil
	}
	k, err := classify(b, p)
	switch k {
	case kindFile, kindLinkFile:
		return "", fs.notDir()
	case kindLinkDir:
		switch fs.slash {
		case slashLinux:
			return "", fs.notDir()
		case slashDarwin:
			return realPath(b, p, true)
		}
	case kindLinkDangling, kindLinkErr:
		switch {
		case fs.slash != slashDarwin:
			return "", fs.notDir()
		case k == kindLinkErr:
			return "", err
		}
		return "", syscall.ENOENT
	}
	return p, nil
}

// renameDest returns the path a Rename onto newpath (dst in dstB) writes,
// or the errno it fails with, for a source src in srcB. Windows ignores a
// trailing slash on the destination unless its parent is a file. On Unix it
// requires the destination to end up a directory: an existing
// non-directory is ENOTDIR, and a missing destination for a non-directory
// source is ENOTDIR on Linux and ENOENT on macOS, which also renames a
// directory onto the target of a dangling symlink. A symlink to a directory
// is EEXIST, as os.Rename refuses any destination that Lstat, which follows
// the slash, finds is a directory, unless it is the source itself; a
// directory renamed onto itself is left alone (errRenameSelf), except that
// Linux refuses to put it at the link's name with ENOTDIR. A link that
// can't be followed fails as following it does on macOS (ELOOP) and as
// not-a-directory on Linux.
func (fs *SwitchFS) renameDest(newpath string, srcB absfs.FileSystem, src string, dstB absfs.FileSystem, dst string) (string, error) {
	if !hasTrailingSlash(newpath) {
		return dst, nil
	}
	if fs.slash == slashWindows {
		// The slash is ignored, except that below a file it makes the
		// name not-a-directory (ERROR_INVALID_PARAMETER without it).
		if parent, err := dstB.Stat(path.Dir(dst)); err == nil && !parent.IsDir() {
			return "", fs.notDir()
		}
		return dst, nil
	}
	srcInfo, err := lstat(srcB, src)
	if err != nil {
		return dst, nil // the rename reports the missing source
	}
	k, kerr := classify(dstB, dst)
	switch {
	case k == kindFile || k == kindLinkFile:
		return "", fs.notDir()
	case k == kindLinkErr && fs.slash == slashDarwin:
		return "", kerr
	case k == kindLinkErr:
		return "", fs.notDir()
	case (k == kindDir || k == kindLinkDir) && sameBackend(srcB, dstB) && sameEntry(dstB, src, dst):
		if k == kindLinkDir && fs.slash == slashLinux {
			return "", fs.notDir()
		}
		return "", errRenameSelf
	case k == kindLinkDir:
		return "", syscall.EEXIST
	case k != kindMissing && k != kindLinkDangling:
		return dst, nil
	case fs.slash == slashLinux:
		if k == kindMissing && srcInfo.IsDir() {
			return dst, nil
		}
		return "", fs.notDir()
	case !srcInfo.IsDir():
		return "", syscall.ENOENT
	case k == kindLinkDangling:
		return realPath(dstB, dst, true)
	}
	return dst, nil
}

// symlinkSlash returns the errno Symlink fails with when newname, at p in
// b, has a trailing slash, or nil to let the backend decide (which fails
// with EEXIST when p exists).
func (fs *SwitchFS) symlinkSlash(newname string, b absfs.FileSystem, p string) error {
	if !hasTrailingSlash(newname) {
		return nil
	}
	k, err := classify(b, p)
	switch fs.slash {
	case slashLinux:
		if k == kindMissing {
			return syscall.ENOENT
		}
	case slashDarwin:
		switch {
		case fs.lookupNotDir(k):
			return fs.notDir()
		case k == kindLinkErr:
			return err
		case k == kindMissing || k == kindLinkDangling:
			return syscall.ENOENT
		}
	case slashWindows:
		if k == kindMissing || fs.lookupNotDir(k) {
			return fs.notDir()
		}
	}
	return nil
}

// readlinkSlash returns the errno Readlink fails with when name, at p in b,
// has a trailing slash, or nil to let the backend read the link. Unix
// resolves the slash through the link, so what's left is never a link:
// EINVAL for a directory, ENOENT for nothing, and whatever stopped the
// resolution (ELOOP) for a link that can't be followed. Windows reads the
// link.
func (fs *SwitchFS) readlinkSlash(name string, b absfs.FileSystem, p string) error {
	if !hasTrailingSlash(name) {
		return nil
	}
	k, err := classify(b, p)
	if fs.lookupNotDir(k) {
		return fs.notDir()
	}
	if fs.slash == slashWindows {
		return nil
	}
	switch k {
	case kindDir, kindLinkDir:
		return syscall.EINVAL
	case kindMissing, kindLinkDangling:
		return syscall.ENOENT
	case kindLinkErr:
		return err
	}
	return nil
}

// ownerChecksSlash reports whether Chown and Lchown apply a trailing slash.
// On Windows os.Chown and os.Lchown fail without looking at the name, so
// the backend's answer stands.
func (fs *SwitchFS) ownerChecksSlash() bool {
	return fs.slash != slashWindows
}

// hostSlash is the trailing-slash behaviour of the host os package.
var hostSlash = hostSlashRules(runtime.GOOS)
