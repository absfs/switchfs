package switchfs

import (
	"testing"

	"github.com/absfs/absfs"
	"github.com/absfs/memfs"
)

// mustMemFS returns a fresh in-memory filesystem.
func mustMemFS(t testing.TB) *memfs.FileSystem {
	t.Helper()
	m, err := memfs.NewFS()
	if err != nil {
		t.Fatalf("memfs.NewFS: %v", err)
	}
	return m
}

// writeFile creates name in fs with data, failing the test on error.
func writeFile(t testing.TB, fs absfs.FileSystem, name, data string) {
	t.Helper()
	f, err := fs.Create(name)
	if err != nil {
		t.Fatalf("Create(%q): %v", name, err)
	}
	if _, err := f.Write([]byte(data)); err != nil {
		t.Fatalf("Write(%q): %v", name, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close(%q): %v", name, err)
	}
}

// readFile returns the contents of name in fs, failing the test on error.
func readFile(t testing.TB, fs absfs.FileSystem, name string) string {
	t.Helper()
	data, err := fs.ReadFile(name)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", name, err)
	}
	return string(data)
}

// getBackend finds the backend for a virtual path, discarding the rewritten
// path. Production code uses resolve; this is a test convenience.
func (fs *SwitchFS) getBackend(name string) (absfs.FileSystem, error) {
	backend, _, err := fs.getBackendAndRewrite(name, nil)
	return backend, err
}
