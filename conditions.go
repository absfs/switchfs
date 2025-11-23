package switchfs

import (
	"os"
	"time"
)

// RouteCondition evaluates whether a route should match based on file metadata
type RouteCondition interface {
	// Evaluate returns true if the condition is met for the given file info
	Evaluate(path string, info os.FileInfo) bool
}

// PathRewriter rewrites/transforms paths for a route
type PathRewriter interface {
	// Rewrite transforms a path according to route rules
	Rewrite(path string) string
}

// sizeCondition matches files based on size
type sizeCondition struct {
	minSize int64
	maxSize int64
}

func (c *sizeCondition) Evaluate(path string, info os.FileInfo) bool {
	if info == nil {
		return true // Can't evaluate, assume match
	}

	size := info.Size()

	if c.minSize > 0 && size < c.minSize {
		return false
	}

	if c.maxSize > 0 && size > c.maxSize {
		return false
	}

	return true
}

// MinSize creates a condition that matches files >= minSize bytes
func MinSize(bytes int64) RouteCondition {
	return &sizeCondition{minSize: bytes}
}

// MaxSize creates a condition that matches files <= maxSize bytes
func MaxSize(bytes int64) RouteCondition {
	return &sizeCondition{maxSize: bytes}
}

// SizeRange creates a condition that matches files within a size range
func SizeRange(minBytes, maxBytes int64) RouteCondition {
	return &sizeCondition{minSize: minBytes, maxSize: maxBytes}
}

// timeCondition matches files based on modification time
type timeCondition struct {
	olderThan *time.Time
	newerThan *time.Time
}

func (c *timeCondition) Evaluate(path string, info os.FileInfo) bool {
	if info == nil {
		return true // Can't evaluate, assume match
	}

	modTime := info.ModTime()

	if c.olderThan != nil && modTime.After(*c.olderThan) {
		return false
	}

	if c.newerThan != nil && modTime.Before(*c.newerThan) {
		return false
	}

	return true
}

// OlderThan creates a condition that matches files modified before the given time
func OlderThan(t time.Time) RouteCondition {
	return &timeCondition{olderThan: &t}
}

// NewerThan creates a condition that matches files modified after the given time
func NewerThan(t time.Time) RouteCondition {
	return &timeCondition{newerThan: &t}
}

// ModifiedBetween creates a condition that matches files modified within a time range
func ModifiedBetween(start, end time.Time) RouteCondition {
	return &timeCondition{newerThan: &start, olderThan: &end}
}

// directoryCondition matches only directories or only files
type directoryCondition struct {
	directoriesOnly bool
}

func (c *directoryCondition) Evaluate(path string, info os.FileInfo) bool {
	if info == nil {
		return true // Can't evaluate, assume match
	}

	if c.directoriesOnly {
		return info.IsDir()
	}
	return !info.IsDir()
}

// DirectoriesOnly creates a condition that matches only directories
func DirectoriesOnly() RouteCondition {
	return &directoryCondition{directoriesOnly: true}
}

// FilesOnly creates a condition that matches only files (not directories)
func FilesOnly() RouteCondition {
	return &directoryCondition{directoriesOnly: false}
}

// andCondition combines multiple conditions with AND logic
type andCondition struct {
	conditions []RouteCondition
}

func (c *andCondition) Evaluate(path string, info os.FileInfo) bool {
	for _, cond := range c.conditions {
		if !cond.Evaluate(path, info) {
			return false
		}
	}
	return true
}

// And combines multiple conditions - all must be true
func And(conditions ...RouteCondition) RouteCondition {
	return &andCondition{conditions: conditions}
}

// orCondition combines multiple conditions with OR logic
type orCondition struct {
	conditions []RouteCondition
}

func (c *orCondition) Evaluate(path string, info os.FileInfo) bool {
	for _, cond := range c.conditions {
		if cond.Evaluate(path, info) {
			return true
		}
	}
	return false
}

// Or combines multiple conditions - at least one must be true
func Or(conditions ...RouteCondition) RouteCondition {
	return &orCondition{conditions: conditions}
}

// notCondition inverts a condition
type notCondition struct {
	condition RouteCondition
}

func (c *notCondition) Evaluate(path string, info os.FileInfo) bool {
	return !c.condition.Evaluate(path, info)
}

// Not inverts a condition
func Not(condition RouteCondition) RouteCondition {
	return &notCondition{condition: condition}
}
