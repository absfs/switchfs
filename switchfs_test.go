package switchfs

import (
	"io/fs"
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

// TestSwitchFS_Separator removed - Separator() method removed in absfs 1.0
// All absfs filesystems now use Unix-style paths with '/' separator

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
	name          string
	openFileCount int
	mkdirCount    int
	removeCount   int
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

func (m *countingMockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	return nil, nil
}

func (m *countingMockFS) ReadFile(name string) ([]byte, error) {
	return nil, nil
}

func (m *countingMockFS) Sub(dir string) (fs.FS, error) {
	return absfs.FilerToFS(m, dir)
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

// trackingMockFS is a mock that tracks which operations were called
type trackingMockFS struct {
	name           string
	lastOp         string
	lastPath       string
	lastMode       os.FileMode
	lastAtime      time.Time
	lastMtime      time.Time
	lastUid        int
	lastGid        int
	lastSize       int64
	returnErr      error
	returnFileInfo os.FileInfo
}

func (m *trackingMockFS) Open(name string) (absfs.File, error) {
	m.lastOp = "Open"
	m.lastPath = name
	return nil, m.returnErr
}

func (m *trackingMockFS) Create(name string) (absfs.File, error) {
	m.lastOp = "Create"
	m.lastPath = name
	return nil, m.returnErr
}

func (m *trackingMockFS) Remove(name string) error {
	m.lastOp = "Remove"
	m.lastPath = name
	return m.returnErr
}

func (m *trackingMockFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	m.lastOp = "OpenFile"
	m.lastPath = name
	m.lastMode = perm
	return nil, m.returnErr
}

func (m *trackingMockFS) Mkdir(name string, perm os.FileMode) error {
	m.lastOp = "Mkdir"
	m.lastPath = name
	m.lastMode = perm
	return m.returnErr
}

func (m *trackingMockFS) MkdirAll(name string, perm os.FileMode) error {
	m.lastOp = "MkdirAll"
	m.lastPath = name
	m.lastMode = perm
	return m.returnErr
}

func (m *trackingMockFS) Rename(oldpath, newpath string) error {
	m.lastOp = "Rename"
	m.lastPath = oldpath
	return m.returnErr
}

func (m *trackingMockFS) Stat(name string) (os.FileInfo, error) {
	m.lastOp = "Stat"
	m.lastPath = name
	return m.returnFileInfo, m.returnErr
}

func (m *trackingMockFS) Chmod(name string, mode os.FileMode) error {
	m.lastOp = "Chmod"
	m.lastPath = name
	m.lastMode = mode
	return m.returnErr
}

func (m *trackingMockFS) Chtimes(name string, atime time.Time, mtime time.Time) error {
	m.lastOp = "Chtimes"
	m.lastPath = name
	m.lastAtime = atime
	m.lastMtime = mtime
	return m.returnErr
}

func (m *trackingMockFS) Chown(name string, uid, gid int) error {
	m.lastOp = "Chown"
	m.lastPath = name
	m.lastUid = uid
	m.lastGid = gid
	return m.returnErr
}

func (m *trackingMockFS) RemoveAll(path string) error {
	m.lastOp = "RemoveAll"
	m.lastPath = path
	return m.returnErr
}

func (m *trackingMockFS) Truncate(name string, size int64) error {
	m.lastOp = "Truncate"
	m.lastPath = name
	m.lastSize = size
	return m.returnErr
}

func (m *trackingMockFS) ReadDir(name string) ([]fs.DirEntry, error) {
	m.lastOp = "ReadDir"
	m.lastPath = name
	return nil, m.returnErr
}

func (m *trackingMockFS) ReadFile(name string) ([]byte, error) {
	m.lastOp = "ReadFile"
	m.lastPath = name
	return nil, m.returnErr
}

func (m *trackingMockFS) Sub(dir string) (fs.FS, error) {
	m.lastOp = "Sub"
	m.lastPath = dir
	if m.returnErr != nil {
		return nil, m.returnErr
	}
	return absfs.FilerToFS(m, dir)
}

func (m *trackingMockFS) Chdir(dir string) error { return nil }
func (m *trackingMockFS) Getwd() (string, error) { return "/", nil }
func (m *trackingMockFS) TempDir() string        { return "/tmp" }

func TestSwitchFS_Mkdir(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("successful mkdir", func(t *testing.T) {
		err := fs.Mkdir("/data/newdir", 0755)
		if err != nil {
			t.Errorf("Mkdir() error = %v", err)
		}
		if backend.lastOp != "Mkdir" {
			t.Errorf("lastOp = %v, want Mkdir", backend.lastOp)
		}
		if backend.lastPath != "/data/newdir" {
			t.Errorf("lastPath = %v, want /data/newdir", backend.lastPath)
		}
		if backend.lastMode != 0755 {
			t.Errorf("lastMode = %v, want 0755", backend.lastMode)
		}
	})

	t.Run("mkdir with no route", func(t *testing.T) {
		err := fs.Mkdir("/unrouted/dir", 0755)
		if err != ErrNoRoute {
			t.Errorf("Mkdir() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("mkdir with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrExist
		err := fs.Mkdir("/data/exists", 0755)
		if err != os.ErrExist {
			t.Errorf("Mkdir() error = %v, want ErrExist", err)
		}
		backend.returnErr = nil
	})
}

func TestSwitchFS_MkdirAll(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("successful mkdirall", func(t *testing.T) {
		err := fs.MkdirAll("/data/deep/nested/dir", 0755)
		if err != nil {
			t.Errorf("MkdirAll() error = %v", err)
		}
		if backend.lastOp != "MkdirAll" {
			t.Errorf("lastOp = %v, want MkdirAll", backend.lastOp)
		}
		if backend.lastPath != "/data/deep/nested/dir" {
			t.Errorf("lastPath = %v, want /data/deep/nested/dir", backend.lastPath)
		}
	})

	t.Run("mkdirall with no route", func(t *testing.T) {
		err := fs.MkdirAll("/unrouted/dir", 0755)
		if err != ErrNoRoute {
			t.Errorf("MkdirAll() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("mkdirall with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrPermission
		err := fs.MkdirAll("/data/noperm", 0755)
		if err != os.ErrPermission {
			t.Errorf("MkdirAll() error = %v, want ErrPermission", err)
		}
		backend.returnErr = nil
	})
}

func TestSwitchFS_Remove(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("successful remove", func(t *testing.T) {
		err := fs.Remove("/data/file.txt")
		if err != nil {
			t.Errorf("Remove() error = %v", err)
		}
		if backend.lastOp != "Remove" {
			t.Errorf("lastOp = %v, want Remove", backend.lastOp)
		}
		if backend.lastPath != "/data/file.txt" {
			t.Errorf("lastPath = %v, want /data/file.txt", backend.lastPath)
		}
	})

	t.Run("remove with no route", func(t *testing.T) {
		err := fs.Remove("/unrouted/file.txt")
		if err != ErrNoRoute {
			t.Errorf("Remove() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("remove with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrNotExist
		err := fs.Remove("/data/notexist.txt")
		if err != os.ErrNotExist {
			t.Errorf("Remove() error = %v, want ErrNotExist", err)
		}
		backend.returnErr = nil
	})
}

func TestSwitchFS_RemoveAll(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("successful removeall", func(t *testing.T) {
		err := fs.RemoveAll("/data/directory")
		if err != nil {
			t.Errorf("RemoveAll() error = %v", err)
		}
		if backend.lastOp != "RemoveAll" {
			t.Errorf("lastOp = %v, want RemoveAll", backend.lastOp)
		}
		if backend.lastPath != "/data/directory" {
			t.Errorf("lastPath = %v, want /data/directory", backend.lastPath)
		}
	})

	t.Run("removeall with no route", func(t *testing.T) {
		err := fs.RemoveAll("/unrouted/dir")
		if err != ErrNoRoute {
			t.Errorf("RemoveAll() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("removeall with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrPermission
		err := fs.RemoveAll("/data/noperm")
		if err != os.ErrPermission {
			t.Errorf("RemoveAll() error = %v, want ErrPermission", err)
		}
		backend.returnErr = nil
	})
}

func TestSwitchFS_Stat(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("successful stat", func(t *testing.T) {
		mockInfo := &statMockFileInfo{name: "test.txt", size: 100}
		backend.returnFileInfo = mockInfo

		info, err := fs.Stat("/data/test.txt")
		if err != nil {
			t.Errorf("Stat() error = %v", err)
		}
		if backend.lastOp != "Stat" {
			t.Errorf("lastOp = %v, want Stat", backend.lastOp)
		}
		if backend.lastPath != "/data/test.txt" {
			t.Errorf("lastPath = %v, want /data/test.txt", backend.lastPath)
		}
		if info != mockInfo {
			t.Errorf("Stat() returned wrong FileInfo")
		}
	})

	t.Run("stat with no route", func(t *testing.T) {
		_, err := fs.Stat("/unrouted/file.txt")
		if err != ErrNoRoute {
			t.Errorf("Stat() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("stat with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrNotExist
		backend.returnFileInfo = nil
		_, err := fs.Stat("/data/notexist.txt")
		if err != os.ErrNotExist {
			t.Errorf("Stat() error = %v, want ErrNotExist", err)
		}
		backend.returnErr = nil
	})
}

func TestSwitchFS_Chmod(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("successful chmod", func(t *testing.T) {
		err := fs.Chmod("/data/file.txt", 0644)
		if err != nil {
			t.Errorf("Chmod() error = %v", err)
		}
		if backend.lastOp != "Chmod" {
			t.Errorf("lastOp = %v, want Chmod", backend.lastOp)
		}
		if backend.lastPath != "/data/file.txt" {
			t.Errorf("lastPath = %v, want /data/file.txt", backend.lastPath)
		}
		if backend.lastMode != 0644 {
			t.Errorf("lastMode = %v, want 0644", backend.lastMode)
		}
	})

	t.Run("chmod with no route", func(t *testing.T) {
		err := fs.Chmod("/unrouted/file.txt", 0644)
		if err != ErrNoRoute {
			t.Errorf("Chmod() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("chmod with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrPermission
		err := fs.Chmod("/data/noperm.txt", 0644)
		if err != os.ErrPermission {
			t.Errorf("Chmod() error = %v, want ErrPermission", err)
		}
		backend.returnErr = nil
	})
}

func TestSwitchFS_Chtimes(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	now := time.Now()
	atime := now.Add(-1 * time.Hour)
	mtime := now

	t.Run("successful chtimes", func(t *testing.T) {
		err := fs.Chtimes("/data/file.txt", atime, mtime)
		if err != nil {
			t.Errorf("Chtimes() error = %v", err)
		}
		if backend.lastOp != "Chtimes" {
			t.Errorf("lastOp = %v, want Chtimes", backend.lastOp)
		}
		if backend.lastPath != "/data/file.txt" {
			t.Errorf("lastPath = %v, want /data/file.txt", backend.lastPath)
		}
		if !backend.lastAtime.Equal(atime) {
			t.Errorf("lastAtime = %v, want %v", backend.lastAtime, atime)
		}
		if !backend.lastMtime.Equal(mtime) {
			t.Errorf("lastMtime = %v, want %v", backend.lastMtime, mtime)
		}
	})

	t.Run("chtimes with no route", func(t *testing.T) {
		err := fs.Chtimes("/unrouted/file.txt", atime, mtime)
		if err != ErrNoRoute {
			t.Errorf("Chtimes() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("chtimes with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrNotExist
		err := fs.Chtimes("/data/notexist.txt", atime, mtime)
		if err != os.ErrNotExist {
			t.Errorf("Chtimes() error = %v, want ErrNotExist", err)
		}
		backend.returnErr = nil
	})
}

func TestSwitchFS_Chown(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("successful chown", func(t *testing.T) {
		err := fs.Chown("/data/file.txt", 1000, 1000)
		if err != nil {
			t.Errorf("Chown() error = %v", err)
		}
		if backend.lastOp != "Chown" {
			t.Errorf("lastOp = %v, want Chown", backend.lastOp)
		}
		if backend.lastPath != "/data/file.txt" {
			t.Errorf("lastPath = %v, want /data/file.txt", backend.lastPath)
		}
		if backend.lastUid != 1000 {
			t.Errorf("lastUid = %v, want 1000", backend.lastUid)
		}
		if backend.lastGid != 1000 {
			t.Errorf("lastGid = %v, want 1000", backend.lastGid)
		}
	})

	t.Run("chown with no route", func(t *testing.T) {
		err := fs.Chown("/unrouted/file.txt", 1000, 1000)
		if err != ErrNoRoute {
			t.Errorf("Chown() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("chown with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrPermission
		err := fs.Chown("/data/noperm.txt", 1000, 1000)
		if err != os.ErrPermission {
			t.Errorf("Chown() error = %v, want ErrPermission", err)
		}
		backend.returnErr = nil
	})
}

func TestSwitchFS_Truncate(t *testing.T) {
	backend := &trackingMockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithPriority(100)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("successful truncate", func(t *testing.T) {
		err := fs.Truncate("/data/file.txt", 100)
		if err != nil {
			t.Errorf("Truncate() error = %v", err)
		}
		if backend.lastOp != "Truncate" {
			t.Errorf("lastOp = %v, want Truncate", backend.lastOp)
		}
		if backend.lastPath != "/data/file.txt" {
			t.Errorf("lastPath = %v, want /data/file.txt", backend.lastPath)
		}
		if backend.lastSize != 100 {
			t.Errorf("lastSize = %v, want 100", backend.lastSize)
		}
	})

	t.Run("truncate with no route", func(t *testing.T) {
		err := fs.Truncate("/unrouted/file.txt", 100)
		if err != ErrNoRoute {
			t.Errorf("Truncate() error = %v, want ErrNoRoute", err)
		}
	})

	t.Run("truncate with backend error", func(t *testing.T) {
		backend.returnErr = os.ErrNotExist
		err := fs.Truncate("/data/notexist.txt", 100)
		if err != os.ErrNotExist {
			t.Errorf("Truncate() error = %v, want ErrNotExist", err)
		}
		backend.returnErr = nil
	})
}

// TestSwitchFS_ListSeparator removed - ListSeparator() method removed in absfs 1.0
// All absfs filesystems now use ':' as the list separator constant

func TestWithRouter(t *testing.T) {
	t.Run("custom router", func(t *testing.T) {
		customRouter := NewRouter()
		backend := &mockFS{name: "test"}
		customRouter.AddRoute(Route{
			Pattern:  "/custom",
			Backend:  backend,
			Priority: 100,
			Type:     PatternPrefix,
		})

		fs, err := New(WithRouter(customRouter))
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}

		// Verify custom router is used
		got, err := fs.getBackend("/custom/path")
		if err != nil {
			t.Fatalf("getBackend() error = %v", err)
		}
		if got != backend {
			t.Errorf("getBackend() should use custom router")
		}
	})

	t.Run("nil router returns error", func(t *testing.T) {
		_, err := New(WithRouter(nil))
		if err != ErrNilBackend {
			t.Errorf("New(WithRouter(nil)) error = %v, want ErrNilBackend", err)
		}
	})
}

// statMockFileInfo for testing in this file
type statMockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (m *statMockFileInfo) Name() string       { return m.name }
func (m *statMockFileInfo) Size() int64        { return m.size }
func (m *statMockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *statMockFileInfo) ModTime() time.Time { return m.modTime }
func (m *statMockFileInfo) IsDir() bool        { return m.isDir }
func (m *statMockFileInfo) Sys() interface{}   { return nil }
