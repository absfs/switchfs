package switchfs

import (
	"os"
	"testing"
	"time"

	"github.com/absfs/absfs"
)

func TestNew(t *testing.T) {
	backend := &mockFS{name: "test"}

	tests := []struct {
		name    string
		opts    []Option
		wantErr bool
	}{
		{
			name:    "empty switchfs",
			opts:    []Option{},
			wantErr: false,
		},
		{
			name: "with default backend",
			opts: []Option{
				WithDefault(backend),
			},
			wantErr: false,
		},
		{
			name: "with route",
			opts: []Option{
				WithRoute("/data", backend, WithPriority(100)),
			},
			wantErr: false,
		},
		{
			name: "with multiple routes",
			opts: []Option{
				WithRoute("/hot", backend, WithPriority(100)),
				WithRoute("/warm", backend, WithPriority(50)),
				WithRoute("/cold", backend, WithPriority(10)),
			},
			wantErr: false,
		},
		{
			name: "nil default backend",
			opts: []Option{
				WithDefault(nil),
			},
			wantErr: true,
		},
		{
			name: "nil route backend",
			opts: []Option{
				WithRoute("/data", nil),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs, err := New(tt.opts...)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && fs == nil {
				t.Error("New() returned nil filesystem")
			}
		})
	}
}

func TestSwitchFS_GetBackend(t *testing.T) {
	backend1 := &mockFS{name: "backend1"}
	backend2 := &mockFS{name: "backend2"}
	defaultBackend := &mockFS{name: "default"}

	fs, err := New(
		WithRoute("/data", backend1, WithPriority(100)),
		WithRoute("*.txt", backend2, WithPriority(50), WithPatternType(PatternGlob)),
		WithDefault(defaultBackend),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name        string
		path        string
		wantBackend absfs.FileSystem
		wantErr     bool
	}{
		{
			name:        "route to backend1",
			path:        "/data/file.dat",
			wantBackend: backend1,
			wantErr:     false,
		},
		{
			name:        "route to backend2 via glob",
			path:        "/other/file.txt",
			wantBackend: backend2,
			wantErr:     false,
		},
		{
			name:        "route to default backend",
			path:        "/unmatched/path.bin",
			wantBackend: defaultBackend,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := fs.getBackend(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("getBackend() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantBackend {
				t.Errorf("getBackend() = %v, want %v", got, tt.wantBackend)
			}
		})
	}
}

func TestSwitchFS_NoDefaultBackend(t *testing.T) {
	backend := &mockFS{name: "backend"}

	fs, err := New(
		WithRoute("/data", backend),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Should return error for unmatched path
	_, err = fs.getBackend("/unmatched/path")
	if err != ErrNoRoute {
		t.Errorf("getBackend() error = %v, want ErrNoRoute", err)
	}
}

func TestSwitchFS_Chdir(t *testing.T) {
	fs, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name    string
		dir     string
		want    string
		wantErr bool
	}{
		{
			name:    "absolute path",
			dir:     "/home/user",
			want:    "/home/user",
			wantErr: false,
		},
		{
			name:    "relative path",
			dir:     "subdir",
			want:    "/subdir",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset to root
			fs.Chdir("/")

			err := fs.Chdir(tt.dir)
			if (err != nil) != tt.wantErr {
				t.Errorf("Chdir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				got, _ := fs.Getwd()
				if got != tt.want {
					t.Errorf("Getwd() = %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestSwitchFS_Separator(t *testing.T) {
	fs, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := fs.Separator(); got != '/' {
		t.Errorf("Separator() = %v, want /", got)
	}

	// Test custom separator
	fs2, err := New(WithSeparator('\\'))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := fs2.Separator(); got != '\\' {
		t.Errorf("Separator() = %v, want \\", got)
	}
}

func TestSwitchFS_TempDir(t *testing.T) {
	fs, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := fs.TempDir(); got != "/tmp" {
		t.Errorf("TempDir() = %v, want /tmp", got)
	}

	// Test custom temp dir
	fs2, err := New(WithTempDir("/custom/tmp"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := fs2.TempDir(); got != "/custom/tmp" {
		t.Errorf("TempDir() = %v, want /custom/tmp", got)
	}
}

func TestSwitchFS_Router(t *testing.T) {
	fs, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	router := fs.Router()
	if router == nil {
		t.Error("Router() returned nil")
	}

	// Verify we can add routes through the router
	backend := &mockFS{name: "test"}
	err = router.AddRoute(Route{
		Pattern:  "/test",
		Backend:  backend,
		Priority: 100,
		Type:     PatternPrefix,
	})
	if err != nil {
		t.Errorf("Router().AddRoute() error = %v", err)
	}

	// Verify the route works
	got, err := fs.getBackend("/test/file.txt")
	if err != nil {
		t.Fatalf("getBackend() error = %v", err)
	}
	if got != backend {
		t.Errorf("getBackend() = %v, want backend", got)
	}
}

func TestOptions(t *testing.T) {
	backend := &mockFS{name: "test"}

	t.Run("WithPriority", func(t *testing.T) {
		route := Route{}
		opt := WithPriority(100)
		if err := opt(&route); err != nil {
			t.Errorf("WithPriority() error = %v", err)
		}
		if route.Priority != 100 {
			t.Errorf("Priority = %v, want 100", route.Priority)
		}
	})

	t.Run("WithPatternType", func(t *testing.T) {
		route := Route{}
		opt := WithPatternType(PatternGlob)
		if err := opt(&route); err != nil {
			t.Errorf("WithPatternType() error = %v", err)
		}
		if route.Type != PatternGlob {
			t.Errorf("Type = %v, want PatternGlob", route.Type)
		}
	})

	t.Run("WithFailover", func(t *testing.T) {
		route := Route{}
		opt := WithFailover(backend)
		if err := opt(&route); err != nil {
			t.Errorf("WithFailover() error = %v", err)
		}
		if route.Failover != backend {
			t.Errorf("Failover = %v, want backend", route.Failover)
		}
	})

	t.Run("WithFailover nil", func(t *testing.T) {
		route := Route{}
		opt := WithFailover(nil)
		if err := opt(&route); err != ErrNilBackend {
			t.Errorf("WithFailover(nil) error = %v, want ErrNilBackend", err)
		}
	})
}

func TestPatternType_String(t *testing.T) {
	tests := []struct {
		pt   PatternType
		want string
	}{
		{PatternPrefix, "prefix"},
		{PatternGlob, "glob"},
		{PatternRegex, "regex"},
		{PatternType(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.pt.String(); got != tt.want {
				t.Errorf("PatternType.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

// countingMockFS is a mock that counts operations
type countingMockFS struct {
	name         string
	openFileCount int
	mkdirCount   int
	removeCount  int
}

func (m *countingMockFS) Open(name string) (absfs.File, error) {
	return nil, nil
}

func (m *countingMockFS) Create(name string) (absfs.File, error) {
	return nil, nil
}

func (m *countingMockFS) Remove(name string) error {
	m.removeCount++
	return nil
}

func (m *countingMockFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	m.openFileCount++
	return nil, nil
}

func (m *countingMockFS) Mkdir(name string, perm os.FileMode) error {
	return nil
}

func (m *countingMockFS) Rename(oldpath, newpath string) error {
	return nil
}

func (m *countingMockFS) Stat(name string) (os.FileInfo, error) {
	return nil, nil
}

func (m *countingMockFS) Chmod(name string, mode os.FileMode) error {
	return nil
}

func (m *countingMockFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return nil
}

func (m *countingMockFS) Chown(name string, uid, gid int) error {
	return nil
}

func (m *countingMockFS) Separator() uint8 {
	return '/'
}

func (m *countingMockFS) ListSeparator() uint8 {
	return ':'
}

func (m *countingMockFS) Chdir(dir string) error {
	return nil
}

func (m *countingMockFS) Getwd() (string, error) {
	return "/", nil
}

func (m *countingMockFS) TempDir() string {
	return "/tmp"
}

func (m *countingMockFS) MkdirAll(name string, perm os.FileMode) error {
	return nil
}

func (m *countingMockFS) RemoveAll(path string) error {
	return nil
}

func (m *countingMockFS) Truncate(name string, size int64) error {
	return nil
}

func TestSwitchFS_OperationRouting(t *testing.T) {
	backend1 := &countingMockFS{name: "backend1"}
	backend2 := &countingMockFS{name: "backend2"}

	fs, err := New(
		WithRoute("/data1", backend1, WithPriority(100)),
		WithRoute("/data2", backend2, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	// Operations should route to correct backend
	fs.Open("/data1/file.txt")
	if backend1.openFileCount != 1 {
		t.Errorf("backend1.openFileCount = %v, want 1", backend1.openFileCount)
	}
	if backend2.openFileCount != 0 {
		t.Errorf("backend2.openFileCount = %v, want 0", backend2.openFileCount)
	}

	fs.Create("/data2/file.txt")
	if backend2.openFileCount != 1 {
		t.Errorf("backend2.openFileCount = %v, want 1", backend2.openFileCount)
	}
	if backend1.openFileCount != 1 {
		t.Errorf("backend1.openFileCount = %v, want 1", backend1.openFileCount)
	}
}
