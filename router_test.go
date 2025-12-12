package switchfs

import (
	"io/fs"
	"os"
	"testing"
	"time"

	"github.com/absfs/absfs"
)

// mockFS is a simple mock filesystem for testing
type mockFS struct {
	name string
}

func (m *mockFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	return nil, nil
}

func (m *mockFS) Mkdir(name string, perm os.FileMode) error {
	return nil
}

func (m *mockFS) Remove(name string) error {
	return nil
}

func (m *mockFS) Rename(oldpath, newpath string) error {
	return nil
}

func (m *mockFS) Stat(name string) (os.FileInfo, error) {
	return nil, nil
}

func (m *mockFS) Chmod(name string, mode os.FileMode) error {
	return nil
}

func (m *mockFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return nil
}

func (m *mockFS) Chown(name string, uid, gid int) error {
	return nil
}

func (m *mockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return nil, nil
}

func (m *mockFS) ReadFile(name string) ([]byte, error) {
	return nil, nil
}

func (m *mockFS) Sub(dir string) (fs.FS, error) {
	return absfs.FilerToFS(m, dir)
}

func (m *mockFS) Chdir(dir string) error {
	return nil
}

func (m *mockFS) Getwd() (string, error) {
	return "/", nil
}

func (m *mockFS) TempDir() string {
	return "/tmp"
}

func (m *mockFS) Open(name string) (absfs.File, error) {
	return nil, nil
}

func (m *mockFS) Create(name string) (absfs.File, error) {
	return nil, nil
}

func (m *mockFS) MkdirAll(name string, perm os.FileMode) error {
	return nil
}

func (m *mockFS) RemoveAll(path string) error {
	return nil
}

func (m *mockFS) Truncate(name string, size int64) error {
	return nil
}

func TestRouter_AddRoute(t *testing.T) {
	r := NewRouter()
	backend := &mockFS{name: "test"}

	tests := []struct {
		name    string
		route   Route
		wantErr bool
	}{
		{
			name: "valid prefix route",
			route: Route{
				Pattern:  "/data",
				Backend:  backend,
				Priority: 100,
				Type:     PatternPrefix,
			},
			wantErr: false,
		},
		{
			name: "valid glob route",
			route: Route{
				Pattern:  "*.txt",
				Backend:  backend,
				Priority: 50,
				Type:     PatternGlob,
			},
			wantErr: false,
		},
		{
			name: "nil backend",
			route: Route{
				Pattern:  "/data",
				Backend:  nil,
				Priority: 100,
				Type:     PatternPrefix,
			},
			wantErr: true,
		},
		{
			name: "invalid glob pattern",
			route: Route{
				Pattern:  "[invalid",
				Backend:  backend,
				Priority: 100,
				Type:     PatternGlob,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.AddRoute(tt.route)
			if (err != nil) != tt.wantErr {
				t.Errorf("Router.AddRoute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRouter_Route(t *testing.T) {
	r := NewRouter()
	backend1 := &mockFS{name: "backend1"}
	backend2 := &mockFS{name: "backend2"}
	backend3 := &mockFS{name: "backend3"}

	// Add routes with different priorities
	r.AddRoute(Route{
		Pattern:  "/hot",
		Backend:  backend1,
		Priority: 100,
		Type:     PatternPrefix,
	})

	r.AddRoute(Route{
		Pattern:  "/warm",
		Backend:  backend2,
		Priority: 50,
		Type:     PatternPrefix,
	})

	r.AddRoute(Route{
		Pattern:  "*.txt",
		Backend:  backend3,
		Priority: 10,
		Type:     PatternGlob,
	})

	tests := []struct {
		name        string
		path        string
		wantBackend absfs.FileSystem
		wantErr     bool
	}{
		{
			name:        "match high priority route",
			path:        "/hot/cache.dat",
			wantBackend: backend1,
			wantErr:     false,
		},
		{
			name:        "match medium priority route",
			path:        "/warm/data.bin",
			wantBackend: backend2,
			wantErr:     false,
		},
		{
			name:        "match glob route",
			path:        "/other/file.txt",
			wantBackend: backend3,
			wantErr:     false,
		},
		{
			name:        "no matching route",
			path:        "/cold/archive.zip",
			wantBackend: nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.Route(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Router.Route() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantBackend {
				t.Errorf("Router.Route() = %v, want %v", got, tt.wantBackend)
			}
		})
	}
}

func TestRouter_PriorityOrdering(t *testing.T) {
	r := NewRouter()
	lowPriority := &mockFS{name: "low"}
	highPriority := &mockFS{name: "high"}

	// Add low priority first - matches all .txt files
	r.AddRoute(Route{
		Pattern:  "*.txt",
		Backend:  lowPriority,
		Priority: 10,
		Type:     PatternGlob,
	})

	// Add high priority second - matches /data prefix (should match first)
	r.AddRoute(Route{
		Pattern:  "/data",
		Backend:  highPriority,
		Priority: 100,
		Type:     PatternPrefix,
	})

	// High priority prefix should win over low priority glob
	backend, err := r.Route("/data/file.txt")
	if err != nil {
		t.Fatalf("Router.Route() error = %v", err)
	}
	if backend != highPriority {
		t.Errorf("Router.Route() = %v, want high priority backend", backend)
	}
}

func TestRouter_RemoveRoute(t *testing.T) {
	r := NewRouter()
	backend := &mockFS{name: "test"}

	// Add a route
	r.AddRoute(Route{
		Pattern:  "/data",
		Backend:  backend,
		Priority: 100,
		Type:     PatternPrefix,
	})

	// Verify it exists
	_, err := r.Route("/data/file.txt")
	if err != nil {
		t.Fatalf("Route should exist before removal")
	}

	// Remove it
	err = r.RemoveRoute("/data")
	if err != nil {
		t.Fatalf("Router.RemoveRoute() error = %v", err)
	}

	// Verify it's gone
	_, err = r.Route("/data/file.txt")
	if err != ErrNoRoute {
		t.Errorf("Route should not exist after removal")
	}

	// Try to remove non-existent route
	err = r.RemoveRoute("/nonexistent")
	if err != ErrNoRoute {
		t.Errorf("RemoveRoute should return ErrNoRoute for non-existent pattern")
	}
}

func TestRouter_Routes(t *testing.T) {
	r := NewRouter()
	backend1 := &mockFS{name: "backend1"}
	backend2 := &mockFS{name: "backend2"}

	// Add routes
	r.AddRoute(Route{
		Pattern:  "/data1",
		Backend:  backend1,
		Priority: 100,
		Type:     PatternPrefix,
	})

	r.AddRoute(Route{
		Pattern:  "/data2",
		Backend:  backend2,
		Priority: 50,
		Type:     PatternPrefix,
	})

	routes := r.Routes()
	if len(routes) != 2 {
		t.Errorf("Routes() returned %d routes, want 2", len(routes))
	}

	// Verify routes are ordered by priority
	if routes[0].Priority < routes[1].Priority {
		t.Errorf("Routes should be ordered by priority (highest first)")
	}
}

func TestRouter_DuplicateRoute(t *testing.T) {
	r := NewRouter()
	backend := &mockFS{name: "test"}

	route := Route{
		Pattern:  "/data",
		Backend:  backend,
		Priority: 100,
		Type:     PatternPrefix,
	}

	// Add route first time
	err := r.AddRoute(route)
	if err != nil {
		t.Fatalf("First AddRoute() error = %v", err)
	}

	// Try to add duplicate
	err = r.AddRoute(route)
	if err != ErrDuplicateRoute {
		t.Errorf("AddRoute() should return ErrDuplicateRoute, got %v", err)
	}
}
