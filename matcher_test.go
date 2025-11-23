package switchfs

import (
	"testing"
)

func TestPrefixMatcher(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
	}{
		{
			name:    "exact match",
			pattern: "/data",
			path:    "/data",
			want:    true,
		},
		{
			name:    "prefix match",
			pattern: "/data",
			path:    "/data/file.txt",
			want:    true,
		},
		{
			name:    "no match",
			pattern: "/data",
			path:    "/other/file.txt",
			want:    false,
		},
		{
			name:    "prefix with wildcard path",
			pattern: "/tmp",
			path:    "/tmp/cache/data.txt",
			want:    true,
		},
		{
			name:    "relative prefix",
			pattern: "data",
			path:    "data/file.txt",
			want:    true,
		},
		{
			name:    "no leading slash match",
			pattern: "tmp",
			path:    "/tmp/file.txt",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := newPrefixMatcher(tt.pattern)
			if err != nil {
				t.Fatalf("newPrefixMatcher() error = %v", err)
			}
			if got := m.Match(tt.path); got != tt.want {
				t.Errorf("prefixMatcher.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGlobMatcher(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
		wantErr bool
	}{
		{
			name:    "simple wildcard",
			pattern: "*.txt",
			path:    "file.txt",
			want:    true,
		},
		{
			name:    "no match extension",
			pattern: "*.txt",
			path:    "file.json",
			want:    false,
		},
		{
			name:    "double star pattern",
			pattern: "/data/**/*.txt",
			path:    "/data/subdir/file.txt",
			want:    true,
		},
		{
			name:    "double star deep path",
			pattern: "/data/**/*.txt",
			path:    "/data/a/b/c/file.txt",
			want:    true,
		},
		{
			name:    "pattern with multiple extensions",
			pattern: "*.{txt,json,xml}",
			path:    "data.json",
			want:    true,
		},
		{
			name:    "cache directory pattern",
			pattern: "**/.cache/*",
			path:    "/home/user/.cache/data",
			want:    true,
		},
		{
			name:    "wildcard in middle",
			pattern: "/data/*/output.txt",
			path:    "/data/project1/output.txt",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := newGlobMatcher(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Fatalf("newGlobMatcher() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got := m.Match(tt.path); got != tt.want {
				t.Errorf("globMatcher.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRegexMatcher(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		path    string
		want    bool
		wantErr bool
	}{
		{
			name:    "simple regex",
			pattern: "^/data/.*\\.txt$",
			path:    "/data/file.txt",
			want:    true,
		},
		{
			name:    "no match",
			pattern: "^/data/.*\\.txt$",
			path:    "/other/file.txt",
			want:    false,
		},
		{
			name:    "regex with alternatives",
			pattern: "^/data/.+\\.(txt|json|xml)$",
			path:    "/data/file.json",
			want:    true,
		},
		{
			name:    "invalid regex",
			pattern: "[invalid",
			path:    "/data/file.txt",
			wantErr: true,
		},
		{
			name:    "complex pattern",
			pattern: "^/(hot|warm|cold)/.*$",
			path:    "/hot/cache.dat",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := newRegexMatcher(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Fatalf("newRegexMatcher() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got := m.Match(tt.path); got != tt.want {
				t.Errorf("regexMatcher.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompileMatcher(t *testing.T) {
	tests := []struct {
		name        string
		pattern     string
		patternType PatternType
		testPath    string
		want        bool
		wantErr     bool
	}{
		{
			name:        "prefix type",
			pattern:     "/data",
			patternType: PatternPrefix,
			testPath:    "/data/file.txt",
			want:        true,
		},
		{
			name:        "glob type",
			pattern:     "*.txt",
			patternType: PatternGlob,
			testPath:    "file.txt",
			want:        true,
		},
		{
			name:        "regex type",
			pattern:     "^/data/.*\\.txt$",
			patternType: PatternRegex,
			testPath:    "/data/file.txt",
			want:        true,
		},
		{
			name:        "invalid pattern type",
			pattern:     "/data",
			patternType: PatternType(99),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := compileMatcher(tt.pattern, tt.patternType)
			if (err != nil) != tt.wantErr {
				t.Fatalf("compileMatcher() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got := m.Match(tt.testPath); got != tt.want {
				t.Errorf("matcher.Match() = %v, want %v", got, tt.want)
			}
		})
	}
}
