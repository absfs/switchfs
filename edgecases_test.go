package switchfs

import (
	"errors"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/absfs/absfs"
	"github.com/absfs/memfs"
)

// Phase 4: Edge Cases and Error Handling

func TestErrNoRoute_AllOperations(t *testing.T) {
	// Create a SwitchFS with no routes and no default backend
	fs, err := New()
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		name string
		op   func() error
	}{
		{"Open", func() error { _, e := fs.Open("/path"); return e }},
		{"Create", func() error { _, e := fs.Create("/path"); return e }},
		{"OpenFile", func() error { _, e := fs.OpenFile("/path", os.O_RDONLY, 0); return e }},
		{"Mkdir", func() error { return fs.Mkdir("/path", 0755) }},
		{"MkdirAll", func() error { return fs.MkdirAll("/path", 0755) }},
		{"Remove", func() error { return fs.Remove("/path") }},
		{"RemoveAll", func() error { return fs.RemoveAll("/path") }},
		{"Stat", func() error { _, e := fs.Stat("/path"); return e }},
		{"Chmod", func() error { return fs.Chmod("/path", 0644) }},
		{"Chtimes", func() error { return fs.Chtimes("/path", time.Now(), time.Now()) }},
		{"Chown", func() error { return fs.Chown("/path", 0, 0) }},
		{"Truncate", func() error { return fs.Truncate("/path", 0) }},
		{"Rename", func() error { return fs.Rename("/path", "/newpath") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.op()
			if err != ErrNoRoute {
				t.Errorf("%s returned %v, want ErrNoRoute", tt.name, err)
			}
		})
	}
}

func TestErrNilBackend_AllOptions(t *testing.T) {
	tests := []struct {
		name string
		opts []Option
	}{
		{"WithDefault(nil)", []Option{WithDefault(nil)}},
		{"WithRoute with nil backend", []Option{WithRoute("/data", nil)}},
		{"WithRouter(nil)", []Option{WithRouter(nil)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.opts...)
			if err != ErrNilBackend {
				t.Errorf("New() error = %v, want ErrNilBackend", err)
			}
		})
	}
}

func TestPatternMatcher_EdgeCases(t *testing.T) {
	backend := &mockFS{name: "test"}

	tests := []struct {
		name        string
		pattern     string
		patternType PatternType
		testPath    string
		wantMatch   bool
	}{
		// Very long paths
		{
			name:        "very long path matches prefix",
			pattern:     "/data",
			patternType: PatternPrefix,
			testPath:    "/data/" + string(make([]byte, 1000)),
			wantMatch:   true,
		},
		// Special characters
		{
			name:        "path with spaces",
			pattern:     "/my data",
			patternType: PatternPrefix,
			testPath:    "/my data/file.txt",
			wantMatch:   true,
		},
		{
			name:        "path with special chars",
			pattern:     "/data-v2.0",
			patternType: PatternPrefix,
			testPath:    "/data-v2.0/file.txt",
			wantMatch:   true,
		},
		// Unicode
		{
			name:        "unicode path",
			pattern:     "/données",
			patternType: PatternPrefix,
			testPath:    "/données/fichier.txt",
			wantMatch:   true,
		},
		{
			name:        "emoji in path",
			pattern:     "/📁data",
			patternType: PatternPrefix,
			testPath:    "/📁data/file.txt",
			wantMatch:   true,
		},
		// Glob edge cases
		{
			name:        "glob double star",
			pattern:     "/data/**/file.txt",
			patternType: PatternGlob,
			testPath:    "/data/a/b/c/file.txt",
			wantMatch:   true,
		},
		{
			name:        "glob question mark",
			pattern:     "/data/file?.txt",
			patternType: PatternGlob,
			testPath:    "/data/file1.txt",
			wantMatch:   true,
		},
		// Regex edge cases
		{
			name:        "regex complex pattern",
			pattern:     `^/v\d+/api/.*$`,
			patternType: PatternRegex,
			testPath:    "/v1/api/users",
			wantMatch:   true,
		},
		{
			name:        "regex with anchors",
			pattern:     `^/data/`,
			patternType: PatternRegex,
			testPath:    "/data/file.txt",
			wantMatch:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter()
			err := router.AddRoute(Route{
				Pattern:  tt.pattern,
				Backend:  backend,
				Priority: 100,
				Type:     tt.patternType,
			})
			if err != nil {
				t.Fatalf("AddRoute error = %v", err)
			}

			_, err = router.Route(tt.testPath)
			gotMatch := err == nil

			if gotMatch != tt.wantMatch {
				t.Errorf("Route(%q) match = %v, want %v (err=%v)", tt.testPath, gotMatch, tt.wantMatch, err)
			}
		})
	}
}

func TestInvalidPatterns(t *testing.T) {
	backend := &mockFS{name: "test"}

	tests := []struct {
		name        string
		pattern     string
		patternType PatternType
	}{
		{"invalid glob - unclosed bracket", "[invalid", PatternGlob},
		{"invalid regex - unclosed paren", "(invalid", PatternRegex},
		{"invalid regex - bad escape", `\`, PatternRegex},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter()
			err := router.AddRoute(Route{
				Pattern:  tt.pattern,
				Backend:  backend,
				Priority: 100,
				Type:     tt.patternType,
			})
			if err == nil {
				t.Errorf("AddRoute() should fail for invalid pattern %q", tt.pattern)
			}
		})
	}
}

func TestConcurrentRouteAccess(t *testing.T) {
	router := NewRouter()
	backend := &mockFS{name: "test"}

	// Add initial routes
	for i := 0; i < 10; i++ {
		router.AddRoute(Route{
			Pattern:  "/data" + string(rune('0'+i)),
			Backend:  backend,
			Priority: i,
			Type:     PatternPrefix,
		})
	}

	var wg sync.WaitGroup
	errChan := make(chan error, 100)

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				path := "/data" + string(rune('0'+(n%10))) + "/file.txt"
				_, err := router.Route(path)
				if err != nil && err != ErrNoRoute {
					errChan <- err
				}
			}
		}(i)
	}

	// Concurrent route listing
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = router.Routes()
			}
		}()
	}

	wg.Wait()
	close(errChan)

	for err := range errChan {
		t.Errorf("Concurrent access error: %v", err)
	}
}

func TestCrossBackendMove_EdgeCases(t *testing.T) {
	t.Run("empty directory move", func(t *testing.T) {
		backend1, _ := memfs.NewFS()
		backend2, _ := memfs.NewFS()

		fs, _ := New(
			WithRoute("/src", backend1, WithPriority(100)),
			WithRoute("/dst", backend2, WithPriority(100)),
		)

		// Create empty directory
		backend1.MkdirAll("/src/emptydir", 0755)

		// Create destination parent
		backend2.MkdirAll("/dst", 0755)

		// Move empty directory
		err := fs.Rename("/src/emptydir", "/dst/emptydir")
		if err != nil {
			t.Fatalf("Rename() error = %v", err)
		}

		// Verify directory exists at destination
		info, err := backend2.Stat("/dst/emptydir")
		if err != nil {
			t.Errorf("Directory not found at destination: %v", err)
		}
		if info != nil && !info.IsDir() {
			t.Error("Expected directory at destination")
		}

		// Verify source removed
		_, err = backend1.Stat("/src/emptydir")
		if err == nil {
			t.Error("Directory should be removed from source")
		}
	})
}

// errorMockFS returns errors for specific operations
type errorMockFS struct {
	mockFS
	openFileErr error
	createErr   error
	statErr     error
	statInfo    os.FileInfo
}

func (m *errorMockFS) OpenFile(name string, flag int, perm os.FileMode) (absfs.File, error) {
	if m.openFileErr != nil {
		return nil, m.openFileErr
	}
	return nil, nil
}

func (m *errorMockFS) Open(name string) (absfs.File, error) {
	return m.OpenFile(name, os.O_RDONLY, 0)
}

func (m *errorMockFS) Create(name string) (absfs.File, error) {
	if m.createErr != nil {
		return nil, m.createErr
	}
	return nil, nil
}

func (m *errorMockFS) Stat(name string) (os.FileInfo, error) {
	if m.statErr != nil {
		return nil, m.statErr
	}
	return m.statInfo, nil
}

func TestCrossBackendMove_ErrorScenarios(t *testing.T) {
	t.Run("source stat error", func(t *testing.T) {
		srcBackend := &errorMockFS{statErr: os.ErrNotExist}
		dstBackend := &mockFS{name: "dst"}

		fs, _ := New(
			WithRoute("/src", srcBackend, WithPriority(100)),
			WithRoute("/dst", dstBackend, WithPriority(100)),
		)

		err := fs.Rename("/src/file.txt", "/dst/file.txt")
		if err == nil {
			t.Error("Rename() should fail when source stat fails")
		}
	})
}

func TestOpenFile_ErrorPropagation(t *testing.T) {
	customErr := errors.New("custom backend error")
	backend := &errorMockFS{openFileErr: customErr}

	fs, _ := New(
		WithRoute("/data", backend, WithPriority(100)),
	)

	_, err := fs.Open("/data/file.txt")
	if err != customErr {
		t.Errorf("Open() error = %v, want custom error", err)
	}
}

func TestRename_SameBackend(t *testing.T) {
	backend, _ := memfs.NewFS()

	fs, _ := New(
		WithRoute("/data", backend, WithPriority(100)),
	)

	// Create source directory
	backend.MkdirAll("/data/src", 0755)

	// Create a file
	f, _ := backend.Create("/data/src/file.txt")
	f.Write([]byte("content"))
	f.Close()

	// Rename within same backend
	err := fs.Rename("/data/src/file.txt", "/data/src/renamed.txt")
	if err != nil {
		t.Fatalf("Rename() error = %v", err)
	}

	// Verify new file exists
	_, err = backend.Stat("/data/src/renamed.txt")
	if err != nil {
		t.Errorf("File not found at new location: %v", err)
	}

	// Verify old file removed
	_, err = backend.Stat("/data/src/file.txt")
	if err == nil {
		t.Error("File should be removed from old location")
	}
}

func TestRouteWithInfo_ConditionNotMet(t *testing.T) {
	backend := &mockFS{name: "test"}
	router := NewRouter()

	// Route that only matches files > 1MB
	router.AddRoute(Route{
		Pattern:   "/data",
		Backend:   backend,
		Priority:  100,
		Type:      PatternPrefix,
		Condition: MinSize(1024 * 1024), // 1MB
	})

	// Try with small file
	smallFile := &mockFileInfo{size: 100}
	_, err := router.RouteWithInfo("/data/file.txt", smallFile)
	if err != ErrNoRoute {
		t.Errorf("RouteWithInfo() should return ErrNoRoute for file not meeting condition, got %v", err)
	}
}

func TestMultipleRoutesWithDifferentPatternTypes(t *testing.T) {
	backend1 := &mockFS{name: "prefix"}
	backend2 := &mockFS{name: "glob"}
	backend3 := &mockFS{name: "regex"}

	fs, err := New(
		WithRoute("/data", backend1, WithPriority(100)),                                     // prefix
		WithRoute("*.log", backend2, WithPriority(90), WithPatternType(PatternGlob)),        // glob
		WithRoute(`^/api/v\d+/`, backend3, WithPriority(80), WithPatternType(PatternRegex)), // regex
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	tests := []struct {
		path        string
		wantBackend absfs.FileSystem
	}{
		{"/data/file.txt", backend1},
		{"/logs/app.log", backend2},
		{"/api/v1/users", backend3},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got, err := fs.getBackend(tt.path)
			if err != nil {
				t.Fatalf("getBackend() error = %v", err)
			}
			if got != tt.wantBackend {
				t.Errorf("getBackend(%s) got wrong backend", tt.path)
			}
		})
	}
}

func TestPriorityOrderingWithManyRoutes(t *testing.T) {
	router := NewRouter()
	backends := make([]*mockFS, 10)

	// Add routes with different priorities and different patterns
	priorities := []int{50, 100, 25, 75, 10, 90, 30, 60, 80, 40}
	for i, p := range priorities {
		backends[i] = &mockFS{name: string(rune('A' + i))}
		router.AddRoute(Route{
			Pattern:  "/data" + string(rune('0'+i)),
			Backend:  backends[i],
			Priority: p,
			Type:     PatternPrefix,
		})
	}

	// Verify routes are sorted by priority (highest first)
	routes := router.Routes()
	for i := 1; i < len(routes); i++ {
		if routes[i].Priority > routes[i-1].Priority {
			t.Errorf("Routes not sorted by priority: %d > %d at index %d", routes[i].Priority, routes[i-1].Priority, i)
		}
	}

	// Verify highest priority route is first
	if routes[0].Priority != 100 {
		t.Errorf("First route should have highest priority (100), got %d", routes[0].Priority)
	}

	// Verify the backend with priority 100 (backends[1]) is accessible
	got, err := router.Route("/data1/file.txt")
	if err != nil {
		t.Fatalf("Route() error = %v", err)
	}
	if got != backends[1] {
		t.Errorf("Route() should return backend with pattern /data1")
	}
}

// Phase 5: Benchmarks

func BenchmarkRouterRoute(b *testing.B) {
	router := NewRouter()
	backend := &mockFS{name: "test"}

	// Add various routes
	router.AddRoute(Route{Pattern: "/hot", Backend: backend, Priority: 100, Type: PatternPrefix})
	router.AddRoute(Route{Pattern: "/warm", Backend: backend, Priority: 50, Type: PatternPrefix})
	router.AddRoute(Route{Pattern: "/cold", Backend: backend, Priority: 10, Type: PatternPrefix})
	router.AddRoute(Route{Pattern: "*.log", Backend: backend, Priority: 5, Type: PatternGlob})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.Route("/hot/data/file.txt")
	}
}

func BenchmarkRouterRoute_Glob(b *testing.B) {
	router := NewRouter()
	backend := &mockFS{name: "test"}

	router.AddRoute(Route{Pattern: "*.txt", Backend: backend, Priority: 100, Type: PatternGlob})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.Route("/data/file.txt")
	}
}

func BenchmarkRouterRoute_Regex(b *testing.B) {
	router := NewRouter()
	backend := &mockFS{name: "test"}

	router.AddRoute(Route{Pattern: `^/api/v\d+/`, Backend: backend, Priority: 100, Type: PatternRegex})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.Route("/api/v1/users")
	}
}

func BenchmarkPrefixMatcher(b *testing.B) {
	matcher, _ := newPrefixMatcher("/data/files")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.Match("/data/files/subdir/file.txt")
	}
}

func BenchmarkGlobMatcher(b *testing.B) {
	matcher, _ := newGlobMatcher("**/*.txt")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.Match("/data/files/subdir/file.txt")
	}
}

func BenchmarkRegexMatcher(b *testing.B) {
	matcher, _ := newRegexMatcher(`^/data/.*/.*\.txt$`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		matcher.Match("/data/files/subdir/file.txt")
	}
}

func BenchmarkConditionEvaluation(b *testing.B) {
	cond := And(
		MinSize(1000),
		MaxSize(10000),
		FilesOnly(),
	)
	info := &mockFileInfo{size: 5000, isDir: false}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cond.Evaluate("/data/file.txt", info)
	}
}

func BenchmarkRewriterChain(b *testing.B) {
	chain := ChainRewriters(
		StripPrefix("/old"),
		AddPrefix("/new"),
		ReplacePrefix("/new/data", "/storage"),
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chain.Rewrite("/old/data/file.txt")
	}
}

func BenchmarkCrossBackendMove_SmallFile(b *testing.B) {
	b.Skip("Skipping benchmark that modifies filesystem state")
}

// Example tests for documentation

func ExampleNew() {
	backend1, _ := memfs.NewFS()
	backend2, _ := memfs.NewFS()

	// Create SwitchFS with multiple backends
	fs, _ := New(
		WithRoute("/hot", backend1, WithPriority(100)),
		WithRoute("/cold", backend2, WithPriority(50)),
	)

	// Files under /hot go to backend1
	f, _ := fs.Create("/hot/cache.dat")
	f.Write([]byte("cached data"))
	f.Close()

	// Files under /cold go to backend2
	f, _ = fs.Create("/cold/archive.dat")
	f.Write([]byte("archived data"))
	f.Close()
}

func ExampleWithCondition() {
	backend, _ := memfs.NewFS()

	// Route only large files to this backend
	_, _ = New(
		WithRoute("/data", backend,
			WithPriority(100),
			WithCondition(MinSize(1024*1024)), // Files >= 1MB
		),
	)
}

func ExampleChainRewriters() {
	// Create a chain that transforms paths
	chain := ChainRewriters(
		StripPrefix("/virtual"),
		AddPrefix("/real/storage"),
	)

	// /virtual/file.txt -> /real/storage/file.txt
	result := chain.Rewrite("/virtual/file.txt")
	_ = result // Use result
}

// mockFile implements absfs.File for testing
type mockFile struct {
	content []byte
	pos     int64
}

func (f *mockFile) Close() error                                   { return nil }
func (f *mockFile) Read(p []byte) (n int, err error)               { return 0, io.EOF }
func (f *mockFile) ReadAt(p []byte, off int64) (n int, err error)  { return 0, io.EOF }
func (f *mockFile) Seek(offset int64, whence int) (int64, error)   { return 0, nil }
func (f *mockFile) Write(p []byte) (n int, err error)              { return len(p), nil }
func (f *mockFile) WriteAt(p []byte, off int64) (n int, err error) { return len(p), nil }
func (f *mockFile) Name() string                                   { return "mockfile" }
func (f *mockFile) Readdir(count int) ([]os.FileInfo, error)       { return nil, nil }
func (f *mockFile) Readdirnames(n int) ([]string, error)           { return nil, nil }
func (f *mockFile) Stat() (os.FileInfo, error)                     { return nil, nil }
func (f *mockFile) Sync() error                                    { return nil }
func (f *mockFile) Truncate(size int64) error                      { return nil }
func (f *mockFile) WriteString(s string) (ret int, err error)      { return len(s), nil }
