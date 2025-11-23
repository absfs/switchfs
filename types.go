package switchfs

import "github.com/absfs/absfs"

// PatternType defines how patterns are matched
type PatternType int

const (
	// PatternPrefix matches by simple prefix
	PatternPrefix PatternType = iota
	// PatternGlob matches using glob patterns
	PatternGlob
	// PatternRegex matches using regular expressions
	PatternRegex
)

// String returns the string representation of PatternType
func (pt PatternType) String() string {
	switch pt {
	case PatternPrefix:
		return "prefix"
	case PatternGlob:
		return "glob"
	case PatternRegex:
		return "regex"
	default:
		return "unknown"
	}
}

// Route defines a routing rule
type Route struct {
	// Pattern is the path pattern (prefix, glob, or regex)
	Pattern string

	// Backend is the target backend filesystem
	Backend absfs.FileSystem

	// Priority determines match order (higher priority routes match first)
	Priority int

	// Type specifies how the pattern should be matched
	Type PatternType

	// Failover is an optional backup backend
	Failover absfs.FileSystem

	// Condition is an optional condition that must be met for routing
	Condition RouteCondition

	// Rewriter optionally transforms paths before passing to backend
	Rewriter PathRewriter

	// compiled stores the compiled pattern matcher
	compiled patternMatcher
}

// Option configures SwitchFS behavior
type Option func(*SwitchFS) error

// RouteOption configures individual routes
type RouteOption func(*Route) error
