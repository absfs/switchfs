package switchfs

import (
	"testing"
)

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		path   string
		want   string
	}{
		{
			name:   "matching prefix removed",
			prefix: "/data",
			path:   "/data/file.txt",
			want:   "/file.txt",
		},
		{
			name:   "non-matching prefix unchanged",
			prefix: "/data",
			path:   "/other/file.txt",
			want:   "/other/file.txt",
		},
		{
			name:   "path equal to prefix",
			prefix: "/data",
			path:   "/data",
			want:   "",
		},
		{
			name:   "empty prefix adds nothing",
			prefix: "",
			path:   "/test/file.txt",
			want:   "/test/file.txt",
		},
		{
			name:   "path without leading slash",
			prefix: "data/",
			path:   "data/file.txt",
			want:   "file.txt",
		},
		{
			name:   "prefix longer than path",
			prefix: "/data/subdir/deep",
			path:   "/data",
			want:   "/data",
		},
		{
			name:   "partial prefix match not stripped",
			prefix: "/datafiles",
			path:   "/data/file.txt",
			want:   "/data/file.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter := StripPrefix(tt.prefix)
			got := rewriter.Rewrite(tt.path)
			if got != tt.want {
				t.Errorf("StripPrefix(%q).Rewrite(%q) = %q, want %q", tt.prefix, tt.path, got, tt.want)
			}
		})
	}
}

func TestAddPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		path   string
		want   string
	}{
		{
			name:   "add prefix to path",
			prefix: "/mnt",
			path:   "/data/file.txt",
			want:   "/mnt/data/file.txt",
		},
		{
			name:   "empty prefix unchanged",
			prefix: "",
			path:   "/test/file.txt",
			want:   "/test/file.txt",
		},
		{
			name:   "add prefix with trailing slash",
			prefix: "/mnt/",
			path:   "file.txt",
			want:   "/mnt/file.txt",
		},
		{
			name:   "add prefix to empty path",
			prefix: "/mnt",
			path:   "",
			want:   "/mnt",
		},
		{
			name:   "add prefix without leading slash",
			prefix: "prefix",
			path:   "/data",
			want:   "prefix/data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter := AddPrefix(tt.prefix)
			got := rewriter.Rewrite(tt.path)
			if got != tt.want {
				t.Errorf("AddPrefix(%q).Rewrite(%q) = %q, want %q", tt.prefix, tt.path, got, tt.want)
			}
		})
	}
}

func TestReplacePrefix(t *testing.T) {
	tests := []struct {
		name      string
		oldPrefix string
		newPrefix string
		path      string
		want      string
	}{
		{
			name:      "replace matching prefix",
			oldPrefix: "/data",
			newPrefix: "/mnt/storage",
			path:      "/data/file.txt",
			want:      "/mnt/storage/file.txt",
		},
		{
			name:      "non-matching prefix unchanged",
			oldPrefix: "/data",
			newPrefix: "/mnt",
			path:      "/other/file.txt",
			want:      "/other/file.txt",
		},
		{
			name:      "empty old prefix adds new prefix",
			oldPrefix: "",
			newPrefix: "/mnt",
			path:      "/data/file.txt",
			want:      "/mnt/data/file.txt",
		},
		{
			name:      "empty new prefix strips old",
			oldPrefix: "/data",
			newPrefix: "",
			path:      "/data/file.txt",
			want:      "/file.txt",
		},
		{
			name:      "both empty unchanged",
			oldPrefix: "",
			newPrefix: "",
			path:      "/test/file.txt",
			want:      "/test/file.txt",
		},
		{
			name:      "replace exact path",
			oldPrefix: "/data",
			newPrefix: "/storage",
			path:      "/data",
			want:      "/storage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter := ReplacePrefix(tt.oldPrefix, tt.newPrefix)
			got := rewriter.Rewrite(tt.path)
			if got != tt.want {
				t.Errorf("ReplacePrefix(%q, %q).Rewrite(%q) = %q, want %q", tt.oldPrefix, tt.newPrefix, tt.path, got, tt.want)
			}
		})
	}
}

func TestRegexRewrite(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		replacement string
		path        string
		want        string
		wantErr     bool
	}{
		{
			name:        "simple pattern replacement",
			pattern:     `\.txt$`,
			replacement: ".md",
			path:        "/docs/file.txt",
			want:        "/docs/file.md",
			wantErr:     false,
		},
		{
			name:        "capture group substitution",
			pattern:     `/user/(\d+)/`,
			replacement: "/users/$1/profile/",
			path:        "/user/123/data",
			want:        "/users/123/profile/data",
			wantErr:     false,
		},
		{
			name:        "multiple matches",
			pattern:     `_`,
			replacement: "-",
			path:        "/my_test_file.txt",
			want:        "/my-test-file.txt",
			wantErr:     false,
		},
		{
			name:        "no matches unchanged",
			pattern:     `\.json$`,
			replacement: ".yaml",
			path:        "/docs/file.txt",
			want:        "/docs/file.txt",
			wantErr:     false,
		},
		{
			name:        "complex regex",
			pattern:     `^/v(\d+)/api/`,
			replacement: "/api/v$1/",
			path:        "/v2/api/users",
			want:        "/api/v2/users",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter, err := RegexRewrite(tt.pattern, tt.replacement)
			if (err != nil) != tt.wantErr {
				t.Errorf("RegexRewrite() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			got := rewriter.Rewrite(tt.path)
			if got != tt.want {
				t.Errorf("RegexRewrite().Rewrite(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestRegexRewrite_InvalidPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
	}{
		{
			name:    "unclosed bracket",
			pattern: `[invalid`,
		},
		{
			name:    "invalid escape",
			pattern: `\`,
		},
		{
			name:    "unclosed paren",
			pattern: `(unclosed`,
		},
		{
			name:    "invalid repetition",
			pattern: `*invalid`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := RegexRewrite(tt.pattern, "replacement")
			if err == nil {
				t.Errorf("RegexRewrite(%q) should return error for invalid pattern", tt.pattern)
			}
		})
	}
}

func TestChainRewriters(t *testing.T) {
	tests := []struct {
		name      string
		rewriters []PathRewriter
		path      string
		want      string
	}{
		{
			name:      "empty chain unchanged",
			rewriters: []PathRewriter{},
			path:      "/test/file.txt",
			want:      "/test/file.txt",
		},
		{
			name: "single rewriter in chain",
			rewriters: []PathRewriter{
				StripPrefix("/data"),
			},
			path: "/data/file.txt",
			want: "/file.txt",
		},
		{
			name: "multiple rewriters applied in order",
			rewriters: []PathRewriter{
				StripPrefix("/src"),
				AddPrefix("/dst"),
			},
			path: "/src/file.txt",
			want: "/dst/file.txt",
		},
		{
			name: "three rewriters chained",
			rewriters: []PathRewriter{
				StripPrefix("/old"),
				AddPrefix("/new"),
				ReplacePrefix("/new/path", "/final"),
			},
			path: "/old/path/to/file.txt",
			want: "/final/to/file.txt",
		},
		{
			name: "rewriters with regex",
			rewriters: func() []PathRewriter {
				regex, _ := RegexRewrite(`\.txt$`, ".md")
				return []PathRewriter{
					StripPrefix("/docs"),
					regex,
				}
			}(),
			path: "/docs/readme.txt",
			want: "/readme.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter := ChainRewriters(tt.rewriters...)
			got := rewriter.Rewrite(tt.path)
			if got != tt.want {
				t.Errorf("ChainRewriters().Rewrite(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestStaticMapping(t *testing.T) {
	tests := []struct {
		name    string
		mapping map[string]string
		path    string
		want    string
	}{
		{
			name:    "empty mapping unchanged",
			mapping: map[string]string{},
			path:    "/test/file.txt",
			want:    "/test/file.txt",
		},
		{
			name: "single entry mapping",
			mapping: map[string]string{
				"/old": "/new",
			},
			path: "/old",
			want: "/new",
		},
		{
			name: "multiple entry mapping",
			mapping: map[string]string{
				"/config": "/etc/config",
				"/data":   "/mnt/data",
				"/logs":   "/var/log",
			},
			path: "/data",
			want: "/mnt/data",
		},
		{
			name: "mapped path returns new path",
			mapping: map[string]string{
				"/src/main.go": "/app/main.go",
			},
			path: "/src/main.go",
			want: "/app/main.go",
		},
		{
			name: "unmapped path returns original",
			mapping: map[string]string{
				"/mapped": "/target",
			},
			path: "/unmapped",
			want: "/unmapped",
		},
		{
			name: "exact match required",
			mapping: map[string]string{
				"/data": "/mnt/data",
			},
			path: "/data/file.txt",
			want: "/data/file.txt", // Not matched because exact match required
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter := StaticMapping(tt.mapping)
			got := rewriter.Rewrite(tt.path)
			if got != tt.want {
				t.Errorf("StaticMapping().Rewrite(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestWithRewriterOption(t *testing.T) {
	backend := &mockFS{name: "test"}

	rewriter := StripPrefix("/virtual")

	fs, err := New(
		WithRoute("/virtual", backend, WithRewriter(rewriter)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	routes := fs.Router().Routes()
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}

	if routes[0].Rewriter == nil {
		t.Error("route should have rewriter set")
	}
}

func TestGetBackendAndRewrite(t *testing.T) {
	backend := &mockFS{name: "test"}

	rewriter := ReplacePrefix("/virtual", "/real")

	fs, err := New(
		WithRoute("/virtual", backend, WithRewriter(rewriter)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("path is rewritten", func(t *testing.T) {
		gotBackend, gotPath, err := fs.getBackendAndRewrite("/virtual/file.txt", nil)
		if err != nil {
			t.Fatalf("getBackendAndRewrite() error = %v", err)
		}
		if gotBackend != backend {
			t.Errorf("getBackendAndRewrite() backend = %v, want %v", gotBackend, backend)
		}
		if gotPath != "/real/file.txt" {
			t.Errorf("getBackendAndRewrite() path = %q, want %q", gotPath, "/real/file.txt")
		}
	})

	t.Run("no matching route uses default", func(t *testing.T) {
		defaultBackend := &mockFS{name: "default"}
		fs2, _ := New(
			WithRoute("/virtual", backend, WithRewriter(rewriter)),
			WithDefault(defaultBackend),
		)

		gotBackend, gotPath, err := fs2.getBackendAndRewrite("/other/path", nil)
		if err != nil {
			t.Fatalf("getBackendAndRewrite() error = %v", err)
		}
		if gotBackend != defaultBackend {
			t.Errorf("getBackendAndRewrite() should use default backend")
		}
		if gotPath != "/other/path" {
			t.Errorf("getBackendAndRewrite() path should be unchanged for default backend")
		}
	})

	t.Run("no matching route and no default returns error", func(t *testing.T) {
		fs3, _ := New(
			WithRoute("/virtual", backend),
		)

		_, _, err := fs3.getBackendAndRewrite("/other/path", nil)
		if err != ErrNoRoute {
			t.Errorf("getBackendAndRewrite() error = %v, want ErrNoRoute", err)
		}
	})
}

func TestRewriterWithCondition(t *testing.T) {
	backend := &mockFS{name: "backend"}
	rewriter := ReplacePrefix("/data", "/storage")

	fs, err := New(
		WithRoute("/data", backend,
			WithPriority(100),
			WithCondition(MinSize(1000)),
			WithRewriter(rewriter)),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	t.Run("large file gets rewriter applied", func(t *testing.T) {
		largeFile := &mockFileInfo{size: 2000}
		gotBackend, gotPath, err := fs.getBackendAndRewrite("/data/file.bin", largeFile)
		if err != nil {
			t.Fatalf("getBackendAndRewrite() error = %v", err)
		}
		if gotBackend != backend {
			t.Errorf("large file should route to backend")
		}
		if gotPath != "/storage/file.bin" {
			t.Errorf("path = %q, want %q", gotPath, "/storage/file.bin")
		}
	})

	t.Run("small file does not match condition", func(t *testing.T) {
		smallFile := &mockFileInfo{size: 500}
		_, _, err := fs.getBackendAndRewrite("/data/file.bin", smallFile)
		if err != ErrNoRoute {
			t.Errorf("small file should not match condition, got err = %v", err)
		}
	})
}

func TestChainRewritersOrder(t *testing.T) {
	// Verify order matters: strip then add vs add then strip
	t.Run("strip then add", func(t *testing.T) {
		chain := ChainRewriters(
			StripPrefix("/old"),
			AddPrefix("/new"),
		)
		got := chain.Rewrite("/old/file.txt")
		if got != "/new/file.txt" {
			t.Errorf("strip-then-add = %q, want %q", got, "/new/file.txt")
		}
	})

	t.Run("add then strip different result", func(t *testing.T) {
		chain := ChainRewriters(
			AddPrefix("/prefix"),
			StripPrefix("/prefix/old"),
		)
		got := chain.Rewrite("/old/file.txt")
		// After AddPrefix: /prefix/old/file.txt
		// After StripPrefix: /file.txt
		if got != "/file.txt" {
			t.Errorf("add-then-strip = %q, want %q", got, "/file.txt")
		}
	})
}

func TestRegexRewriteCaptureGroups(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		replacement string
		path        string
		want        string
	}{
		{
			name:        "single capture group",
			pattern:     `/user/(\w+)/`,
			replacement: "/profile/$1/",
			path:        "/user/john/settings",
			want:        "/profile/john/settings",
		},
		{
			name:        "multiple capture groups",
			pattern:     `/(\d{4})/(\d{2})/(\d{2})/`,
			replacement: "/$3-$2-$1/",
			path:        "/2023/12/25/christmas.txt",
			want:        "/25-12-2023/christmas.txt",
		},
		{
			name:        "named capture groups style",
			pattern:     `/api/v(\d+)/(\w+)`,
			replacement: "/v$1/$2",
			path:        "/api/v2/users",
			want:        "/v2/users",
		},
		{
			name:        "backreference in replacement",
			pattern:     `(\w+)\.(\w+)$`,
			replacement: "$2/$1",
			path:        "/docs/readme.md",
			want:        "/docs/md/readme",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rewriter, err := RegexRewrite(tt.pattern, tt.replacement)
			if err != nil {
				t.Fatalf("RegexRewrite() error = %v", err)
			}
			got := rewriter.Rewrite(tt.path)
			if got != tt.want {
				t.Errorf("RegexRewrite().Rewrite(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestRouteWithRewriter(t *testing.T) {
	backend := &mockFS{name: "storage"}

	r := NewRouter()
	r.AddRoute(Route{
		Pattern:  "/app",
		Backend:  backend,
		Priority: 100,
		Type:     PatternPrefix,
		Rewriter: StripPrefix("/app"),
	})

	t.Run("route includes rewriter", func(t *testing.T) {
		route, err := r.RouteWithInfo("/app/data/file.txt", nil)
		if err != nil {
			t.Fatalf("RouteWithInfo() error = %v", err)
		}
		if route.Rewriter == nil {
			t.Error("route should have rewriter")
		}

		rewritten := route.Rewriter.Rewrite("/app/data/file.txt")
		if rewritten != "/data/file.txt" {
			t.Errorf("rewritten path = %q, want %q", rewritten, "/data/file.txt")
		}
	})
}
