package switchfs

import (
	"os"
	"testing"
	"time"
)

// mockFileInfo implements os.FileInfo for testing conditions
type mockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (m *mockFileInfo) Name() string       { return m.name }
func (m *mockFileInfo) Size() int64        { return m.size }
func (m *mockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mockFileInfo) ModTime() time.Time { return m.modTime }
func (m *mockFileInfo) IsDir() bool        { return m.isDir }
func (m *mockFileInfo) Sys() interface{}   { return nil }

func TestMinSize(t *testing.T) {
	tests := []struct {
		name     string
		minBytes int64
		fileInfo os.FileInfo
		want     bool
	}{
		{
			name:     "file below minimum size",
			minBytes: 1000,
			fileInfo: &mockFileInfo{size: 500},
			want:     false,
		},
		{
			name:     "file at minimum size",
			minBytes: 1000,
			fileInfo: &mockFileInfo{size: 1000},
			want:     true,
		},
		{
			name:     "file above minimum size",
			minBytes: 1000,
			fileInfo: &mockFileInfo{size: 2000},
			want:     true,
		},
		{
			name:     "nil FileInfo assumes match",
			minBytes: 1000,
			fileInfo: nil,
			want:     true,
		},
		{
			name:     "zero size file with non-zero min",
			minBytes: 100,
			fileInfo: &mockFileInfo{size: 0},
			want:     false,
		},
		{
			name:     "zero min size allows any file",
			minBytes: 0,
			fileInfo: &mockFileInfo{size: 0},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := MinSize(tt.minBytes)
			got := cond.Evaluate("/test/path", tt.fileInfo)
			if got != tt.want {
				t.Errorf("MinSize(%d).Evaluate() = %v, want %v", tt.minBytes, got, tt.want)
			}
		})
	}
}

func TestMaxSize(t *testing.T) {
	tests := []struct {
		name     string
		maxBytes int64
		fileInfo os.FileInfo
		want     bool
	}{
		{
			name:     "file below maximum size",
			maxBytes: 1000,
			fileInfo: &mockFileInfo{size: 500},
			want:     true,
		},
		{
			name:     "file at maximum size",
			maxBytes: 1000,
			fileInfo: &mockFileInfo{size: 1000},
			want:     true,
		},
		{
			name:     "file above maximum size",
			maxBytes: 1000,
			fileInfo: &mockFileInfo{size: 2000},
			want:     false,
		},
		{
			name:     "nil FileInfo assumes match",
			maxBytes: 1000,
			fileInfo: nil,
			want:     true,
		},
		{
			name:     "zero size file always matches",
			maxBytes: 100,
			fileInfo: &mockFileInfo{size: 0},
			want:     true,
		},
		{
			name:     "zero max size only matches zero-size files",
			maxBytes: 0,
			fileInfo: &mockFileInfo{size: 100},
			want:     true, // maxSize of 0 means no limit
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := MaxSize(tt.maxBytes)
			got := cond.Evaluate("/test/path", tt.fileInfo)
			if got != tt.want {
				t.Errorf("MaxSize(%d).Evaluate() = %v, want %v", tt.maxBytes, got, tt.want)
			}
		})
	}
}

func TestSizeRange(t *testing.T) {
	tests := []struct {
		name     string
		minBytes int64
		maxBytes int64
		fileInfo os.FileInfo
		want     bool
	}{
		{
			name:     "file below range",
			minBytes: 500,
			maxBytes: 1500,
			fileInfo: &mockFileInfo{size: 100},
			want:     false,
		},
		{
			name:     "file in range",
			minBytes: 500,
			maxBytes: 1500,
			fileInfo: &mockFileInfo{size: 1000},
			want:     true,
		},
		{
			name:     "file above range",
			minBytes: 500,
			maxBytes: 1500,
			fileInfo: &mockFileInfo{size: 2000},
			want:     false,
		},
		{
			name:     "file at min boundary",
			minBytes: 500,
			maxBytes: 1500,
			fileInfo: &mockFileInfo{size: 500},
			want:     true,
		},
		{
			name:     "file at max boundary",
			minBytes: 500,
			maxBytes: 1500,
			fileInfo: &mockFileInfo{size: 1500},
			want:     true,
		},
		{
			name:     "nil FileInfo assumes match",
			minBytes: 500,
			maxBytes: 1500,
			fileInfo: nil,
			want:     true,
		},
		{
			name:     "zero size file",
			minBytes: 0,
			maxBytes: 100,
			fileInfo: &mockFileInfo{size: 0},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := SizeRange(tt.minBytes, tt.maxBytes)
			got := cond.Evaluate("/test/path", tt.fileInfo)
			if got != tt.want {
				t.Errorf("SizeRange(%d, %d).Evaluate() = %v, want %v", tt.minBytes, tt.maxBytes, got, tt.want)
			}
		})
	}
}

func TestOlderThan(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)

	tests := []struct {
		name      string
		threshold time.Time
		fileInfo  os.FileInfo
		want      bool
	}{
		{
			name:      "file modified before threshold (older)",
			threshold: oneHourAgo,
			fileInfo:  &mockFileInfo{modTime: twoHoursAgo},
			want:      true,
		},
		{
			name:      "file modified after threshold (newer)",
			threshold: twoHoursAgo,
			fileInfo:  &mockFileInfo{modTime: oneHourAgo},
			want:      false,
		},
		{
			name:      "file modified at exact threshold",
			threshold: oneHourAgo,
			fileInfo:  &mockFileInfo{modTime: oneHourAgo},
			want:      true, // Not after, so matches
		},
		{
			name:      "nil FileInfo assumes match",
			threshold: oneHourAgo,
			fileInfo:  nil,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := OlderThan(tt.threshold)
			got := cond.Evaluate("/test/path", tt.fileInfo)
			if got != tt.want {
				t.Errorf("OlderThan().Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewerThan(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)

	tests := []struct {
		name      string
		threshold time.Time
		fileInfo  os.FileInfo
		want      bool
	}{
		{
			name:      "file modified before threshold (older)",
			threshold: oneHourAgo,
			fileInfo:  &mockFileInfo{modTime: twoHoursAgo},
			want:      false,
		},
		{
			name:      "file modified after threshold (newer)",
			threshold: twoHoursAgo,
			fileInfo:  &mockFileInfo{modTime: oneHourAgo},
			want:      true,
		},
		{
			name:      "file modified at exact threshold",
			threshold: oneHourAgo,
			fileInfo:  &mockFileInfo{modTime: oneHourAgo},
			want:      true, // Not before, so matches
		},
		{
			name:      "nil FileInfo assumes match",
			threshold: oneHourAgo,
			fileInfo:  nil,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := NewerThan(tt.threshold)
			got := cond.Evaluate("/test/path", tt.fileInfo)
			if got != tt.want {
				t.Errorf("NewerThan().Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestModifiedBetween(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)
	threeHoursAgo := now.Add(-3 * time.Hour)

	tests := []struct {
		name     string
		start    time.Time
		end      time.Time
		fileInfo os.FileInfo
		want     bool
	}{
		{
			name:     "file modified before range",
			start:    twoHoursAgo,
			end:      oneHourAgo,
			fileInfo: &mockFileInfo{modTime: threeHoursAgo},
			want:     false,
		},
		{
			name:     "file modified in range",
			start:    threeHoursAgo,
			end:      oneHourAgo,
			fileInfo: &mockFileInfo{modTime: twoHoursAgo},
			want:     true,
		},
		{
			name:     "file modified after range",
			start:    threeHoursAgo,
			end:      twoHoursAgo,
			fileInfo: &mockFileInfo{modTime: oneHourAgo},
			want:     false,
		},
		{
			name:     "file modified at start boundary",
			start:    twoHoursAgo,
			end:      oneHourAgo,
			fileInfo: &mockFileInfo{modTime: twoHoursAgo},
			want:     true,
		},
		{
			name:     "file modified at end boundary",
			start:    twoHoursAgo,
			end:      oneHourAgo,
			fileInfo: &mockFileInfo{modTime: oneHourAgo},
			want:     true,
		},
		{
			name:     "nil FileInfo assumes match",
			start:    twoHoursAgo,
			end:      oneHourAgo,
			fileInfo: nil,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := ModifiedBetween(tt.start, tt.end)
			got := cond.Evaluate("/test/path", tt.fileInfo)
			if got != tt.want {
				t.Errorf("ModifiedBetween().Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDirectoriesOnly(t *testing.T) {
	tests := []struct {
		name     string
		fileInfo os.FileInfo
		want     bool
	}{
		{
			name:     "directory matches",
			fileInfo: &mockFileInfo{isDir: true},
			want:     true,
		},
		{
			name:     "file does not match",
			fileInfo: &mockFileInfo{isDir: false},
			want:     false,
		},
		{
			name:     "nil FileInfo assumes match",
			fileInfo: nil,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := DirectoriesOnly()
			got := cond.Evaluate("/test/path", tt.fileInfo)
			if got != tt.want {
				t.Errorf("DirectoriesOnly().Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFilesOnly(t *testing.T) {
	tests := []struct {
		name     string
		fileInfo os.FileInfo
		want     bool
	}{
		{
			name:     "file matches",
			fileInfo: &mockFileInfo{isDir: false},
			want:     true,
		},
		{
			name:     "directory does not match",
			fileInfo: &mockFileInfo{isDir: true},
			want:     false,
		},
		{
			name:     "nil FileInfo assumes match",
			fileInfo: nil,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := FilesOnly()
			got := cond.Evaluate("/test/path", tt.fileInfo)
			if got != tt.want {
				t.Errorf("FilesOnly().Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

// trueCondition always returns true
type trueCondition struct{}

func (c *trueCondition) Evaluate(path string, info os.FileInfo) bool {
	return true
}

// falseCondition always returns false
type falseCondition struct{}

func (c *falseCondition) Evaluate(path string, info os.FileInfo) bool {
	return false
}

func TestAnd(t *testing.T) {
	tests := []struct {
		name       string
		conditions []RouteCondition
		want       bool
	}{
		{
			name:       "all conditions true",
			conditions: []RouteCondition{&trueCondition{}, &trueCondition{}, &trueCondition{}},
			want:       true,
		},
		{
			name:       "some conditions false",
			conditions: []RouteCondition{&trueCondition{}, &falseCondition{}, &trueCondition{}},
			want:       false,
		},
		{
			name:       "all conditions false",
			conditions: []RouteCondition{&falseCondition{}, &falseCondition{}, &falseCondition{}},
			want:       false,
		},
		{
			name:       "empty conditions list",
			conditions: []RouteCondition{},
			want:       true,
		},
		{
			name:       "single true condition",
			conditions: []RouteCondition{&trueCondition{}},
			want:       true,
		},
		{
			name:       "single false condition",
			conditions: []RouteCondition{&falseCondition{}},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := And(tt.conditions...)
			got := cond.Evaluate("/test/path", nil)
			if got != tt.want {
				t.Errorf("And().Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOr(t *testing.T) {
	tests := []struct {
		name       string
		conditions []RouteCondition
		want       bool
	}{
		{
			name:       "all conditions true",
			conditions: []RouteCondition{&trueCondition{}, &trueCondition{}, &trueCondition{}},
			want:       true,
		},
		{
			name:       "some conditions true",
			conditions: []RouteCondition{&falseCondition{}, &trueCondition{}, &falseCondition{}},
			want:       true,
		},
		{
			name:       "all conditions false",
			conditions: []RouteCondition{&falseCondition{}, &falseCondition{}, &falseCondition{}},
			want:       false,
		},
		{
			name:       "empty conditions list",
			conditions: []RouteCondition{},
			want:       false,
		},
		{
			name:       "single true condition",
			conditions: []RouteCondition{&trueCondition{}},
			want:       true,
		},
		{
			name:       "single false condition",
			conditions: []RouteCondition{&falseCondition{}},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := Or(tt.conditions...)
			got := cond.Evaluate("/test/path", nil)
			if got != tt.want {
				t.Errorf("Or().Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNot(t *testing.T) {
	tests := []struct {
		name      string
		condition RouteCondition
		want      bool
	}{
		{
			name:      "invert true condition",
			condition: &trueCondition{},
			want:      false,
		},
		{
			name:      "invert false condition",
			condition: &falseCondition{},
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cond := Not(tt.condition)
			got := cond.Evaluate("/test/path", nil)
			if got != tt.want {
				t.Errorf("Not().Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNestedConditions(t *testing.T) {
	tests := []struct {
		name      string
		condition RouteCondition
		want      bool
	}{
		{
			name:      "And inside Or - true",
			condition: Or(And(&trueCondition{}, &trueCondition{}), &falseCondition{}),
			want:      true,
		},
		{
			name:      "And inside Or - false",
			condition: Or(And(&trueCondition{}, &falseCondition{}), &falseCondition{}),
			want:      false,
		},
		{
			name:      "Or inside And - true",
			condition: And(Or(&trueCondition{}, &falseCondition{}), &trueCondition{}),
			want:      true,
		},
		{
			name:      "Or inside And - false",
			condition: And(Or(&falseCondition{}, &falseCondition{}), &trueCondition{}),
			want:      false,
		},
		{
			name:      "Not inside And",
			condition: And(Not(&falseCondition{}), &trueCondition{}),
			want:      true,
		},
		{
			name:      "Not inside Or",
			condition: Or(Not(&trueCondition{}), &trueCondition{}),
			want:      true,
		},
		{
			name:      "complex nested structure",
			condition: And(Or(&trueCondition{}, &falseCondition{}), Not(And(&falseCondition{}, &trueCondition{}))),
			want:      true,
		},
		{
			name:      "deeply nested",
			condition: Not(And(Or(&falseCondition{}, Not(&trueCondition{})), &trueCondition{})),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.condition.Evaluate("/test/path", nil)
			if got != tt.want {
				t.Errorf("nested condition.Evaluate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConditionsWithRealConditions(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	// Test combining real conditions
	t.Run("large and old files", func(t *testing.T) {
		cond := And(MinSize(1000), OlderThan(oneHourAgo))

		// Large and old file matches
		largeOldFile := &mockFileInfo{
			size:    2000,
			modTime: now.Add(-2 * time.Hour),
		}
		if !cond.Evaluate("/test", largeOldFile) {
			t.Error("large and old file should match")
		}

		// Large but new file doesn't match
		largeNewFile := &mockFileInfo{
			size:    2000,
			modTime: now,
		}
		if cond.Evaluate("/test", largeNewFile) {
			t.Error("large but new file should not match")
		}

		// Small and old file doesn't match
		smallOldFile := &mockFileInfo{
			size:    100,
			modTime: now.Add(-2 * time.Hour),
		}
		if cond.Evaluate("/test", smallOldFile) {
			t.Error("small and old file should not match")
		}
	})

	t.Run("file or directory", func(t *testing.T) {
		cond := Or(FilesOnly(), DirectoriesOnly())

		file := &mockFileInfo{isDir: false}
		if !cond.Evaluate("/test", file) {
			t.Error("file should match")
		}

		dir := &mockFileInfo{isDir: true}
		if !cond.Evaluate("/test", dir) {
			t.Error("directory should match")
		}
	})

	t.Run("not large files", func(t *testing.T) {
		cond := Not(MinSize(1000))

		smallFile := &mockFileInfo{size: 500}
		if !cond.Evaluate("/test", smallFile) {
			t.Error("small file should match Not(MinSize)")
		}

		largeFile := &mockFileInfo{size: 2000}
		if cond.Evaluate("/test", largeFile) {
			t.Error("large file should not match Not(MinSize)")
		}
	})
}

func TestRouteWithInfo(t *testing.T) {
	backend := &mockFS{name: "backend"}

	r := NewRouter()

	// Add route for large files only
	r.AddRoute(Route{
		Pattern:   "/data",
		Backend:   backend,
		Priority:  100,
		Type:      PatternPrefix,
		Condition: MinSize(1000),
	})

	t.Run("large file routes to backend", func(t *testing.T) {
		largeFile := &mockFileInfo{size: 2000}
		route, err := r.RouteWithInfo("/data/file.bin", largeFile)
		if err != nil {
			t.Fatalf("RouteWithInfo() error = %v", err)
		}
		if route.Backend != backend {
			t.Errorf("RouteWithInfo() got backend = %v, want backend", route.Backend)
		}
	})

	t.Run("small file does not match condition", func(t *testing.T) {
		smallFile := &mockFileInfo{size: 500}
		_, err := r.RouteWithInfo("/data/file.bin", smallFile)
		// Small file doesn't match condition, so no route
		if err != ErrNoRoute {
			t.Errorf("RouteWithInfo() error = %v, want ErrNoRoute (condition not met)", err)
		}
	})

	t.Run("nil info routes to first match (assumes condition met)", func(t *testing.T) {
		route, err := r.RouteWithInfo("/data/file.bin", nil)
		if err != nil {
			t.Fatalf("RouteWithInfo() error = %v", err)
		}
		// With nil info, condition assumes match
		if route.Backend != backend {
			t.Errorf("RouteWithInfo() with nil info got backend = %v, want backend", route.Backend)
		}
	})

	t.Run("non-matching path returns error", func(t *testing.T) {
		_, err := r.RouteWithInfo("/other/path", nil)
		if err != ErrNoRoute {
			t.Errorf("RouteWithInfo() error = %v, want ErrNoRoute", err)
		}
	})
}

func TestWithConditionOption(t *testing.T) {
	backend := &mockFS{name: "test"}

	fs, err := New(
		WithRoute("/data", backend, WithCondition(MinSize(1000))),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	routes := fs.Router().Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}

	if routes[0].Condition == nil {
		t.Error("route should have condition set")
	}

	// Verify condition works
	largeFile := &mockFileInfo{size: 2000}
	route, err := fs.Router().RouteWithInfo("/data/file.bin", largeFile)
	if err != nil {
		t.Fatalf("RouteWithInfo() error = %v", err)
	}
	if route.Backend != backend {
		t.Error("large file should route to backend")
	}
}

func TestConditionWithDirectoriesOnly(t *testing.T) {
	dirsBackend := &mockFS{name: "dirs"}
	filesBackend := &mockFS{name: "files"}

	r := NewRouter()

	// Use different patterns to avoid duplicate route error
	r.AddRoute(Route{
		Pattern:   "/dirs",
		Backend:   dirsBackend,
		Priority:  100,
		Type:      PatternPrefix,
		Condition: DirectoriesOnly(),
	})

	r.AddRoute(Route{
		Pattern:   "/files",
		Backend:   filesBackend,
		Priority:  100,
		Type:      PatternPrefix,
		Condition: FilesOnly(),
	})

	t.Run("directory routes to dirs backend", func(t *testing.T) {
		dir := &mockFileInfo{isDir: true}
		route, err := r.RouteWithInfo("/dirs/subdir", dir)
		if err != nil {
			t.Fatalf("RouteWithInfo() error = %v", err)
		}
		if route.Backend != dirsBackend {
			t.Errorf("directory should route to dirs backend")
		}
	})

	t.Run("file does not match DirectoriesOnly", func(t *testing.T) {
		file := &mockFileInfo{isDir: false}
		_, err := r.RouteWithInfo("/dirs/file.txt", file)
		if err != ErrNoRoute {
			t.Errorf("file should not match DirectoriesOnly, got err = %v", err)
		}
	})

	t.Run("file routes to files backend", func(t *testing.T) {
		file := &mockFileInfo{isDir: false}
		route, err := r.RouteWithInfo("/files/file.txt", file)
		if err != nil {
			t.Fatalf("RouteWithInfo() error = %v", err)
		}
		if route.Backend != filesBackend {
			t.Errorf("file should route to files backend")
		}
	})

	t.Run("directory does not match FilesOnly", func(t *testing.T) {
		dir := &mockFileInfo{isDir: true}
		_, err := r.RouteWithInfo("/files/subdir", dir)
		if err != ErrNoRoute {
			t.Errorf("directory should not match FilesOnly, got err = %v", err)
		}
	})
}

func TestTimeConditionsWithRouting(t *testing.T) {
	now := time.Now()
	oneHourAgo := now.Add(-1 * time.Hour)

	hotBackend := &mockFS{name: "hot"}
	coldBackend := &mockFS{name: "cold"}

	r := NewRouter()

	// Hot storage for recent files
	r.AddRoute(Route{
		Pattern:   "/hot",
		Backend:   hotBackend,
		Priority:  100,
		Type:      PatternPrefix,
		Condition: NewerThan(oneHourAgo),
	})

	// Cold storage for old files
	r.AddRoute(Route{
		Pattern:   "/cold",
		Backend:   coldBackend,
		Priority:  100,
		Type:      PatternPrefix,
		Condition: OlderThan(oneHourAgo),
	})

	t.Run("recent file routes to hot storage", func(t *testing.T) {
		recentFile := &mockFileInfo{modTime: now}
		route, err := r.RouteWithInfo("/hot/recent.log", recentFile)
		if err != nil {
			t.Fatalf("RouteWithInfo() error = %v", err)
		}
		if route.Backend != hotBackend {
			t.Errorf("recent file should route to hot storage")
		}
	})

	t.Run("old file does not match NewerThan", func(t *testing.T) {
		oldFile := &mockFileInfo{modTime: now.Add(-2 * time.Hour)}
		_, err := r.RouteWithInfo("/hot/old.log", oldFile)
		if err != ErrNoRoute {
			t.Errorf("old file should not match NewerThan, got err = %v", err)
		}
	})

	t.Run("old file routes to cold storage", func(t *testing.T) {
		oldFile := &mockFileInfo{modTime: now.Add(-2 * time.Hour)}
		route, err := r.RouteWithInfo("/cold/old.log", oldFile)
		if err != nil {
			t.Fatalf("RouteWithInfo() error = %v", err)
		}
		if route.Backend != coldBackend {
			t.Errorf("old file should route to cold storage")
		}
	})

	t.Run("recent file does not match OlderThan", func(t *testing.T) {
		recentFile := &mockFileInfo{modTime: now}
		_, err := r.RouteWithInfo("/cold/recent.log", recentFile)
		if err != ErrNoRoute {
			t.Errorf("recent file should not match OlderThan, got err = %v", err)
		}
	})
}
