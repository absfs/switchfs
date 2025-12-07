package switchfs

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// patternMatcher is an interface for different pattern matching strategies
type patternMatcher interface {
	Match(path string) bool
}

// prefixMatcher matches paths by prefix
type prefixMatcher struct {
	prefix string
}

func (m *prefixMatcher) Match(path string) bool {
	// Clean the path to normalize separators
	path = filepath.Clean(path)
	prefix := filepath.Clean(m.prefix)

	// Normalize both to have consistent slash handling
	normPath := path
	normPrefix := prefix

	// Add leading slash if missing
	if !strings.HasPrefix(normPath, "/") {
		normPath = "/" + normPath
	}
	if !strings.HasPrefix(normPrefix, "/") {
		normPrefix = "/" + normPrefix
	}

	// Check if path starts with prefix
	if strings.HasPrefix(normPath, normPrefix) {
		return true
	}

	return false
}

func newPrefixMatcher(pattern string) (*prefixMatcher, error) {
	return &prefixMatcher{prefix: pattern}, nil
}

// globMatcher matches paths using glob patterns
type globMatcher struct {
	pattern string
}

func (m *globMatcher) Match(path string) bool {
	// Clean the path to normalize separators, then convert to forward slashes
	// This ensures consistent matching across platforms (Windows uses backslashes)
	path = filepath.ToSlash(filepath.Clean(path))

	// Use doublestar for glob matching (supports ** and other patterns)
	matched, err := doublestar.Match(m.pattern, path)
	if err != nil {
		return false
	}

	if matched {
		return true
	}

	// Also try matching without leading slash
	if strings.HasPrefix(path, "/") {
		matched, _ = doublestar.Match(m.pattern, path[1:])
		if matched {
			return true
		}
	}

	// Also try matching with leading slash
	if !strings.HasPrefix(path, "/") {
		matched, _ = doublestar.Match(m.pattern, "/"+path)
		if matched {
			return true
		}
	}

	// For simple patterns like "*.txt", also try matching just the basename
	if !strings.Contains(m.pattern, "/") && strings.Contains(path, "/") {
		basename := filepath.Base(path)
		matched, _ = doublestar.Match(m.pattern, basename)
		if matched {
			return true
		}
	}

	return false
}

func newGlobMatcher(pattern string) (*globMatcher, error) {
	// Validate the glob pattern
	if !doublestar.ValidatePattern(pattern) {
		return nil, ErrInvalidPattern
	}
	return &globMatcher{pattern: pattern}, nil
}

// regexMatcher matches paths using regular expressions
type regexMatcher struct {
	regex *regexp.Regexp
}

func (m *regexMatcher) Match(path string) bool {
	return m.regex.MatchString(path)
}

func newRegexMatcher(pattern string) (*regexMatcher, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return nil, ErrInvalidPattern
	}
	return &regexMatcher{regex: regex}, nil
}

// compileMatcher creates a pattern matcher based on the pattern type
func compileMatcher(pattern string, patternType PatternType) (patternMatcher, error) {
	switch patternType {
	case PatternPrefix:
		return newPrefixMatcher(pattern)
	case PatternGlob:
		return newGlobMatcher(pattern)
	case PatternRegex:
		return newRegexMatcher(pattern)
	default:
		return nil, ErrInvalidPattern
	}
}
