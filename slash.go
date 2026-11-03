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

// symlinkLoopLimit bounds how many symlinks resolveLink follows, as the
// kernel's own limit does (40 on Linux, 32 on macOS).
const symlinkLoopLimit = 40

// hasTrailingSlash reports whether name ends in a slash, which requires the
// object it names to be a directory.
func hasTrailingSlash(name string) bool {
	return strings.HasSuffix(name, "/")
}

// slashError reports err, from applying a trailing slash to name, as an
// *os.PathError for op carrying the caller's name. A backend's
// *os.PathError (from following a symlink) keeps its own Op and error.
func slashError(op, name string, err error) error {
	if _, ok := err.(*os.PathError); ok {
		return pathError(err, name)
	}
	return &os.PathError{Op: op, Path: name, Err: err}
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
)

// classify reports what p names in b. A missing name whose parent is not an
// existing directory is kindUnknown, so the backend reports the problem
// with the parent as it would without the slash.
func classify(b absfs.FileSystem, p string) slashKind {
	info, err := lstat(b, p)
	if errors.Is(err, fs.ErrNotExist) {
		if parent, err := b.Stat(path.Dir(p)); err == nil && parent.IsDir() {
			return kindMissing
		}
		return kindUnknown
	}
	if err != nil {
		return kindUnknown
	}
	if info.Mode()&os.ModeSymlink == 0 {
		if info.IsDir() {
			return kindDir
		}
		return kindFile
	}
	info, err = b.Stat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return kindLinkDangling
	case err != nil:
		return kindUnknown
	case info.IsDir():
		return kindLinkDir
	}
	return kindLinkFile
}

// lookupNotDir reports whether looking up a name with a trailing slash
// (stat, open, chmod and the like) fails as not a directory. A dangling
// symlink is not-exist on Unix, where the link is followed, and
// not-a-directory on Windows, where it is not.
func (fs *SwitchFS) lookupNotDir(k slashKind) bool {
	switch k {
	case kindFile, kindLinkFile:
		return true
	case kindLinkDangling:
		return fs.slash == slashWindows
	}
	return false
}

// checkSlash returns an *os.PathError for op when name has a trailing slash
// and p, in b, exists and is not a directory. It returns nil otherwise, and
// the operation goes ahead on p.
func (fs *SwitchFS) checkSlash(op, name string, b absfs.FileSystem, p string) error {
	if !hasTrailingSlash(name) || !fs.lookupNotDir(classify(b, p)) {
		return nil
	}
	return &os.PathError{Op: op, Path: name, Err: fs.notDir()}
}

// resolveLink follows symlinks from p in b until it reaches a path that is
// not a symlink, which may not exist. Relative targets resolve against the
// link's directory.
func resolveLink(b absfs.FileSystem, p string) (string, error) {
	for i := 0; i < symlinkLoopLimit; i++ {
		info, err := lstat(b, p)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			return p, nil
		}
		target, err := b.(absfs.SymLinker).Readlink(p) // lstat only reports symlinks from a SymLinker
		if err != nil {
			return "", err
		}
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(p), target)
		}
		p = target
	}
	return "", syscall.ELOOP
}

// openSlash applies a trailing slash to OpenFile. With O_CREATE, Linux
// refuses any such name with EISDIR before looking at it; macOS reports a
// missing name as ENOENT and Windows as not-a-directory.
func (fs *SwitchFS) openSlash(b absfs.FileSystem, p string, flag int) error {
	k := classify(b, p)
	creating := flag&os.O_CREATE != 0
	switch {
	case creating && fs.slash == slashLinux && k != kindUnknown:
		return syscall.EISDIR
	case fs.lookupNotDir(k):
		return fs.notDir()
	case !creating || (k != kindMissing && k != kindLinkDangling):
		return nil
	case fs.slash == slashWindows:
		return fs.notDir()
	}
	return syscall.ENOENT
}

// mkdir creates p in b for Mkdir(name). On macOS a trailing slash makes
// mkdir fail on a non-directory with ENOTDIR and create the target of a
// dangling symlink; elsewhere the backend's own error (EEXIST) stands.
func (fs *SwitchFS) mkdir(name string, b absfs.FileSystem, p string, perm os.FileMode) error {
	if hasTrailingSlash(name) && fs.slash == slashDarwin {
		switch classify(b, p) {
		case kindFile, kindLinkFile:
			return &os.PathError{Op: "mkdir", Path: name, Err: fs.notDir()}
		case kindLinkDangling:
			target, err := resolveLink(b, p)
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
// Windows on the link itself. A dangling symlink is not-a-directory except
// on macOS, which follows it to ENOENT.
func (fs *SwitchFS) removalPath(name string, b absfs.FileSystem, p string) (string, error) {
	if !hasTrailingSlash(name) {
		return p, nil
	}
	switch classify(b, p) {
	case kindFile, kindLinkFile:
		return "", fs.notDir()
	case kindLinkDir:
		switch fs.slash {
		case slashLinux:
			return "", fs.notDir()
		case slashDarwin:
			return resolveLink(b, p)
		}
	case kindLinkDangling:
		if fs.slash == slashDarwin {
			return "", syscall.ENOENT
		}
		return "", fs.notDir()
	}
	return p, nil
}

// renameDest returns the path a Rename onto newpath (dst in dstB) writes,
// or the errno it fails with, for a source src in srcB. Windows ignores a
// trailing slash on the destination. On Unix it requires the destination to
// end up a directory: an existing non-directory is ENOTDIR, and a missing
// destination for a non-directory source is ENOTDIR on Linux and ENOENT on
// macOS, which also renames a directory onto the target of a dangling
// symlink. A symlink to a directory is EEXIST, as os.Rename refuses any
// destination that Lstat, which follows the slash, finds is a directory.
func (fs *SwitchFS) renameDest(newpath string, srcB absfs.FileSystem, src string, dstB absfs.FileSystem, dst string) (string, error) {
	if !hasTrailingSlash(newpath) || fs.slash == slashWindows {
		return dst, nil
	}
	srcInfo, err := lstat(srcB, src)
	if err != nil {
		return dst, nil // the rename reports the missing source
	}
	k := classify(dstB, dst)
	switch {
	case k == kindFile || k == kindLinkFile:
		return "", fs.notDir()
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
		return resolveLink(dstB, dst)
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
	k := classify(b, p)
	switch fs.slash {
	case slashLinux:
		if k == kindMissing {
			return syscall.ENOENT
		}
	case slashDarwin:
		if fs.lookupNotDir(k) {
			return fs.notDir()
		}
		if k == kindMissing || k == kindLinkDangling {
			return syscall.ENOENT
		}
	case slashWindows:
		switch k {
		case kindFile, kindLinkFile, kindLinkDangling, kindMissing:
			return fs.notDir()
		}
	}
	return nil
}

// readlinkSlash returns the errno Readlink fails with when name, at p in b,
// has a trailing slash, or nil to let the backend read the link. Unix
// resolves the slash through the link, so what's left is never a link:
// EINVAL for a directory, ENOENT for nothing. Windows reads the link.
func (fs *SwitchFS) readlinkSlash(name string, b absfs.FileSystem, p string) error {
	if !hasTrailingSlash(name) {
		return nil
	}
	k := classify(b, p)
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
