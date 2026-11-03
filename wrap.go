package switchfs

import (
	"io/fs"
	"os"
	"path"

	"github.com/absfs/absfs"
)

// pathError reports a backend error against the caller's name. A top-level
// *os.PathError gets the caller's name as its Path, so a rewritten backend
// path never leaks to callers; the wrapped error is kept, so errors.Is and
// os.IsNotExist behave as before. Other errors are returned unchanged.
func pathError(err error, name string) error {
	pe, ok := err.(*os.PathError)
	if !ok || pe.Path == name {
		return err
	}
	return &os.PathError{Op: pe.Op, Path: name, Err: pe.Err}
}

// linkError reports a two-path error (rename, symlink) as an *os.LinkError
// carrying the caller's paths. A *os.PathError from the backend is converted,
// keeping its wrapped error; any other error is wrapped as is.
func linkError(err error, op, oldname, newname string) error {
	switch e := err.(type) {
	case nil:
		return nil
	case *os.LinkError:
		return &os.LinkError{Op: e.Op, Old: oldname, New: newname, Err: e.Err}
	case *os.PathError:
		return &os.LinkError{Op: op, Old: oldname, New: newname, Err: e.Err}
	default:
		return &os.LinkError{Op: op, Old: oldname, New: newname, Err: err}
	}
}

// fileInfo renames a backend FileInfo to the base name of the virtual path.
// Everything else, including Sys, comes from the backend.
type fileInfo struct {
	os.FileInfo
	name string
}

func (fi *fileInfo) Name() string { return fi.name }

// wrapInfo returns info, renamed to the base of vpath when the backend's name
// differs (as it does at a route root rewritten to the backend's "/").
func wrapInfo(info os.FileInfo, vpath string) os.FileInfo {
	if info == nil {
		return nil
	}
	base := path.Base(vpath)
	if info.Name() == base {
		return info
	}
	return &fileInfo{FileInfo: info, name: base}
}

// file presents a backend file under the caller's name: Name returns the name
// passed to OpenFile, Stat reports the virtual base name, and *os.PathError
// values from any method carry the caller's name.
type file struct {
	absfs.File
	name  string // the name as passed to OpenFile
	vpath string // the resolved virtual path
}

// wrapFile returns f unchanged when its Name already matches the caller's
// name, and a renaming wrapper otherwise.
func wrapFile(f absfs.File, name, vpath string) absfs.File {
	if f == nil || f.Name() == name {
		return f
	}
	return &file{File: f, name: name, vpath: vpath}
}

func (f *file) Name() string { return f.name }

func (f *file) Stat() (os.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return info, pathError(err, f.name)
	}
	return wrapInfo(info, f.vpath), nil
}

func (f *file) Read(b []byte) (int, error) {
	n, err := f.File.Read(b)
	return n, pathError(err, f.name)
}

func (f *file) ReadAt(b []byte, off int64) (int, error) {
	n, err := f.File.ReadAt(b, off)
	return n, pathError(err, f.name)
}

func (f *file) Write(b []byte) (int, error) {
	n, err := f.File.Write(b)
	return n, pathError(err, f.name)
}

func (f *file) WriteAt(b []byte, off int64) (int, error) {
	n, err := f.File.WriteAt(b, off)
	return n, pathError(err, f.name)
}

func (f *file) WriteString(s string) (int, error) {
	n, err := f.File.WriteString(s)
	return n, pathError(err, f.name)
}

func (f *file) Seek(offset int64, whence int) (int64, error) {
	n, err := f.File.Seek(offset, whence)
	return n, pathError(err, f.name)
}

func (f *file) Close() error { return pathError(f.File.Close(), f.name) }

func (f *file) Sync() error { return pathError(f.File.Sync(), f.name) }

func (f *file) Truncate(size int64) error { return pathError(f.File.Truncate(size), f.name) }

func (f *file) Readdir(n int) ([]os.FileInfo, error) {
	infos, err := f.File.Readdir(n)
	return infos, pathError(err, f.name)
}

func (f *file) Readdirnames(n int) ([]string, error) {
	names, err := f.File.Readdirnames(n)
	return names, pathError(err, f.name)
}

func (f *file) ReadDir(n int) ([]fs.DirEntry, error) {
	entries, err := f.File.ReadDir(n)
	return entries, pathError(err, f.name)
}
