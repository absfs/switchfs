# switchfs

Path-based routing filesystem for the AbsFs ecosystem. Route filesystem operations to different backend implementations based on path patterns.

## Overview

`switchfs` provides intelligent path-based routing to different filesystem backends. It's designed with a single responsibility: **route operations based on paths**. All other concerns (caching, retry, monitoring, etc.) are handled through composition with other absfs filesystems.

**Core Features:**
- Route operations to different backends by path prefix, glob pattern, or regex
- Support for multiple simultaneous backends (local, cloud, memory, etc.)
- Transparent operation routing - applications see a unified filesystem
- Configurable routing rules with priority ordering
- Cross-backend move/rename with automatic data transfer (files and directories)
- Conditional routing based on file size, modification time, or custom logic
- Path rewriting for backend-specific transformations

**Design Philosophy:**
- **Single Responsibility**: switchfs only handles routing logic
- **Composition Over Integration**: Use other absfs implementations for caching, retry, monitoring, etc.
- **Clean Separation**: Each filesystem wrapper does one thing well

## Use Cases

### Hybrid Storage
Combine local and cloud storage in a single filesystem view:
```go
localFS, _ := osfs.NewFS("/local/storage")
cloudFS := s3fs.NewFS(s3Config)

fs, _ := switchfs.New(
    switchfs.WithRoute("/workspace/*", localFS, switchfs.WithPriority(100)),
    switchfs.WithRoute("/archive/*", cloudFS, switchfs.WithPriority(100)),
    switchfs.WithDefault(localFS),
)
```

### Tiered Storage
Implement hot/warm/cold storage tiers:
```go
hotFS, _ := memfs.NewFS()           // In-memory for hot data
warmFS := osfs.NewFS("/warm")       // Local SSD for warm data
coldFS := s3fs.NewFS(s3Config)      // S3 for cold data

fs, _ := switchfs.New(
    switchfs.WithRoute("/hot/*", hotFS, switchfs.WithPriority(100)),
    switchfs.WithRoute("/warm/*", warmFS, switchfs.WithPriority(50)),
    switchfs.WithRoute("/cold/*", coldFS, switchfs.WithPriority(10)),
)
```

### Multi-Cloud Architecture
Route to different cloud providers by path:
```go
fs, _ := switchfs.New(
    switchfs.WithRoute("/aws/*", s3FS, switchfs.WithPriority(100)),
    switchfs.WithRoute("/gcp/*", gcsFS, switchfs.WithPriority(100)),
    switchfs.WithRoute("/azure/*", azureFS, switchfs.WithPriority(100)),
)
```

## Composition Examples

The power of switchfs comes from composing it with other absfs filesystems. Here are common patterns:

### Example 1: Routing + Caching + Retry + Metrics

```go
import (
    "github.com/absfs/switchfs"
    "github.com/absfs/cachefs"
    "github.com/absfs/retryfs"
    "github.com/absfs/metricsfs"
    "github.com/absfs/memfs"
    "github.com/absfs/osfs"
)

// Create backends
localFS, _ := memfs.NewFS()
remoteFS := s3fs.NewFS(s3Config)

// Layer 1: Route based on paths
router, _ := switchfs.New(
    switchfs.WithRoute("/local/*", localFS, switchfs.WithPriority(100)),
    switchfs.WithRoute("/remote/*", remoteFS, switchfs.WithPriority(100)),
    switchfs.WithDefault(localFS),
)

// Layer 2: Add caching (use cachefs to cache routing decisions and file metadata)
cached := cachefs.New(router, cachefs.WithLRU(1000), cachefs.WithTTL(5*time.Minute))

// Layer 3: Add retry logic (retryfs handles transient failures)
retry := retryfs.New(cached,
    retryfs.WithMaxAttempts(3),
    retryfs.WithExponentialBackoff(100*time.Millisecond, 5*time.Second),
)

// Layer 4: Add metrics collection (metricsfs tracks operations)
metrics := metricsfs.New(retry,
    metricsfs.WithPrometheus(),
    metricsfs.WithOpenTelemetry(),
)

// Use the fully composed filesystem
fs := metrics
file, _ := fs.Open("/remote/data.txt")
```

### Example 2: Routing with Read-Only Enforcement

```go
import (
    "github.com/absfs/switchfs"
    "github.com/absfs/rofs"
)

// Create backends
writeFS := osfs.NewFS("/writable")
readOnlyFS := s3fs.NewFS(s3Config)

// Make the S3 backend explicitly read-only
roFS := rofs.New(readOnlyFS)

// Route with mixed read/write permissions
fs, _ := switchfs.New(
    switchfs.WithRoute("/work/*", writeFS),
    switchfs.WithRoute("/archive/*", roFS),  // Read-only archived data
)
```

### Example 3: Conditional Routing + Compression

```go
import (
    "github.com/absfs/switchfs"
    "github.com/absfs/compressfs"
)

// Large files go to compressed cloud storage
largeFilesFS := compressfs.New(s3FS, compressfs.WithZstd())
smallFilesFS := localFS

fs, _ := switchfs.New(
    // Route large files (>10MB) to compressed cloud storage
    switchfs.WithRoute("/**/*", largeFilesFS,
        switchfs.WithCondition(switchfs.SizeGreaterThan(10*1024*1024)),
        switchfs.WithPriority(100),
        switchfs.WithPatternType(switchfs.PatternGlob),
    ),
    // Everything else to local storage
    switchfs.WithDefault(smallFilesFS),
)
```

### Example 4: Path Rewriting

```go
import (
    "github.com/absfs/switchfs"
)

// Backend stores files with different path structure
backend := s3fs.NewFS(s3Config)

fs, _ := switchfs.New(
    // Rewrite /api/v1/files/* to /production/files/* in backend
    switchfs.WithRoute("/api/v1/files/*", backend,
        switchfs.WithRewriter(switchfs.NewPrefixRewriter("/api/v1/files", "/production/files")),
    ),
)

// Write to /api/v1/files/data.txt
// Actually stored as /production/files/data.txt in S3
file, _ := fs.Create("/api/v1/files/data.txt")
```

### Example 5: Encrypted Cloud Storage with Local Cache

```go
import (
    "github.com/absfs/switchfs"
    "github.com/absfs/encryptfs"
    "github.com/absfs/cachefs"
    "github.com/absfs/unionfs"
)

// Encrypt data before sending to cloud
cloudFS := s3fs.NewFS(s3Config)
encryptedCloud := encryptfs.New(cloudFS, encryptfs.WithAES256GCM(key))

// Local cache for faster access
localCache, _ := memfs.NewFS()

// Union: check local cache first, fall back to encrypted cloud
unified := unionfs.New(localCache, encryptedCloud, unionfs.WithCopyOnWrite())

// Route sensitive data through encrypted cloud
publicFS := osfs.NewFS("/public")

fs, _ := switchfs.New(
    switchfs.WithRoute("/sensitive/*", unified),
    switchfs.WithRoute("/public/*", publicFS),
)
```

## API Design

### Basic Types

```go
// Route defines a routing rule
type Route struct {
    Pattern   string              // Path pattern (prefix, glob, or regex)
    Backend   absfs.FileSystem    // Target backend filesystem
    Priority  int                 // Higher priority routes match first
    Type      PatternType         // Prefix, Glob, or Regex
    Failover  absfs.FileSystem    // Optional backup backend
    Condition RouteCondition      // Optional condition for routing
    Rewriter  PathRewriter        // Optional path transformation
}

// PatternType defines how patterns are matched
type PatternType int

const (
    PatternPrefix PatternType = iota  // Simple prefix match "/data/*"
    PatternGlob                        // Glob pattern "**/*.txt"
    PatternRegex                       // Regular expression "^/user/[0-9]+/.*"
)
```

### Configuration

```go
// Option configures SwitchFS behavior
type Option func(*SwitchFS) error

// WithDefault sets the default backend for unmatched paths
func WithDefault(fs absfs.FileSystem) Option

// WithRoute adds a routing rule
func WithRoute(pattern string, backend absfs.FileSystem, opts ...RouteOption) Option

// RouteOption configures individual routes
type RouteOption func(*Route) error

// WithPriority sets route priority (higher matches first)
func WithPriority(priority int) RouteOption

// WithPatternType sets the pattern matching type
func WithPatternType(pt PatternType) RouteOption

// WithFailover sets a failover backend
func WithFailover(fs absfs.FileSystem) RouteOption

// WithCondition sets a routing condition
func WithCondition(condition RouteCondition) RouteOption

// WithRewriter sets a path rewriter
func WithRewriter(rewriter PathRewriter) RouteOption
```

## Routing Features

### 1. Prefix Matching (Default)
```go
fs, _ := switchfs.New(
    switchfs.WithRoute("/data", backend),
)
// Matches: /data, /data/file.txt, /data/subdir/file.txt
```

### 2. Glob Patterns
```go
fs, _ := switchfs.New(
    // Match all .txt files
    switchfs.WithRoute("**/*.txt", textBackend,
        switchfs.WithPatternType(switchfs.PatternGlob)),

    // Match cache directories anywhere
    switchfs.WithRoute("**/.cache/*", cacheBackend,
        switchfs.WithPatternType(switchfs.PatternGlob)),
)
```

### 3. Regex Patterns
```go
fs, _ := switchfs.New(
    // Match user-specific paths /user/123/files/*
    switchfs.WithRoute(`^/user/[0-9]+/`, userBackend,
        switchfs.WithPatternType(switchfs.PatternRegex)),
)
```

### 4. Priority Ordering
```go
fs, _ := switchfs.New(
    // Specific pattern with high priority
    switchfs.WithRoute("/data/cache/*", cacheBackend,
        switchfs.WithPriority(100)),

    // Broader pattern with lower priority
    switchfs.WithRoute("/data/*", dataBackend,
        switchfs.WithPriority(50)),
)
// /data/cache/file.txt -> routes to cacheBackend (priority 100)
// /data/other/file.txt -> routes to dataBackend (priority 50)
```

### 5. Conditional Routing
```go
// Route based on file size
fs, _ := switchfs.New(
    switchfs.WithRoute("/**/*", largeFileBackend,
        switchfs.WithCondition(switchfs.SizeGreaterThan(100*1024*1024)),
        switchfs.WithPatternType(switchfs.PatternGlob),
        switchfs.WithPriority(100)),
    switchfs.WithDefault(normalBackend),
)

// Custom conditions
fs, _ := switchfs.New(
    switchfs.WithRoute("/**/*", archiveBackend,
        switchfs.WithCondition(switchfs.OlderThan(365*24*time.Hour)),
        switchfs.WithPatternType(switchfs.PatternGlob)),
)
```

### 6. Path Rewriting
```go
// Strip prefix
rewriter := switchfs.NewPrefixRewriter("/api/v1", "")
fs, _ := switchfs.New(
    switchfs.WithRoute("/api/v1/*", backend,
        switchfs.WithRewriter(rewriter)),
)
// /api/v1/file.txt -> stored as /file.txt in backend

// Replace prefix
rewriter := switchfs.NewPrefixRewriter("/old", "/new")
// /old/file.txt -> stored as /new/file.txt
```

### 7. Failover Support
```go
primaryBackend := s3fs.NewFS(primaryConfig)
backupBackend := s3fs.NewFS(backupConfig)

fs, _ := switchfs.New(
    switchfs.WithRoute("/critical/*", primaryBackend,
        switchfs.WithFailover(backupBackend)),
)
// If primary fails, automatically tries backup
```

## Cross-Backend Operations

### File Moves
```go
fs, _ := switchfs.New(
    switchfs.WithRoute("/src/*", backend1),
    switchfs.WithRoute("/dst/*", backend2),
)

// Move file across backends - automatic copy + delete
fs.Rename("/src/file.txt", "/dst/file.txt")
```

### Directory Moves
```go
// Recursively move entire directory tree across backends
fs.Rename("/src/directory", "/dst/directory")
// Automatically copies all files and subdirectories, then removes source
```

### Same-Backend Optimization
```go
// When source and destination use the same backend, native rename is used
fs.Rename("/src/file1.txt", "/src/file2.txt")  // Fast native rename
```

## Available Conditions

Built-in routing conditions:

```go
// Size-based routing
switchfs.SizeGreaterThan(bytes int64)
switchfs.SizeLessThan(bytes int64)
switchfs.SizeBetween(min, max int64)

// Time-based routing
switchfs.ModifiedAfter(t time.Time)
switchfs.ModifiedBefore(t time.Time)
switchfs.OlderThan(duration time.Duration)
switchfs.NewerThan(duration time.Duration)

// Custom conditions
type RouteCondition interface {
    Evaluate(path string, info os.FileInfo) bool
}
```

## Available Rewriters

Built-in path rewriters:

```go
// Prefix replacement
switchfs.NewPrefixRewriter(oldPrefix, newPrefix string)

// Custom rewriters
type PathRewriter interface {
    Rewrite(path string) string
}
```

## Composition Patterns

### Pattern: Caching Layer
```go
router := switchfs.New(...)
cached := cachefs.New(router, opts...)  // Cache on top of routing
```

### Pattern: Retry + Routing
```go
router := switchfs.New(...)
retry := retryfs.New(router, opts...)  // Retry failed operations
```

### Pattern: Metrics + Everything
```go
// Always put metrics at the outermost layer
router := switchfs.New(...)
cached := cachefs.New(router, opts...)
retry := retryfs.New(cached, opts...)
metrics := metricsfs.New(retry, opts...)  // Metrics wraps everything
```

### Pattern: Encryption for Specific Routes
```go
// Encrypt only cloud storage, not local
localFS := osfs.NewFS("/local")
cloudFS := s3fs.NewFS(s3Config)
encryptedCloud := encryptfs.New(cloudFS, opts...)

router := switchfs.New(
    switchfs.WithRoute("/local/*", localFS),
    switchfs.WithRoute("/cloud/*", encryptedCloud),
)
```

## Performance Considerations

### Routing Performance
- Route lookup is O(n) where n = number of routes
- Routes are sorted by priority at registration time
- Glob patterns are compiled once
- Regex patterns are compiled and cached
- **Use `cachefs` to cache routing decisions for frequently accessed paths**

### Cross-Backend Operations
- Move/rename across backends requires data copy
- Large file transfers use streaming (constant memory)
- Directory moves are recursive but efficient
- **For better performance, organize routes to minimize cross-backend moves**

### Optimization Strategies
- Put most frequently accessed routes first (high priority)
- Use prefix patterns when possible (faster than glob/regex)
- Compose with `cachefs` to cache backend lookups
- Use `retryfs` for unreliable backends (cloud, network)
- Use `metricsfs` to identify performance bottlenecks

## Thread Safety

All operations are thread-safe:
- Route registration/removal uses read-write locks
- Concurrent operations routed independently
- Backend-specific concurrency handled by backends
- No shared state between routed operations

## Error Handling

```go
var (
    ErrNoRoute               // No route matches the path
    ErrNilBackend           // Nil backend provided
    ErrInvalidPattern       // Invalid route pattern
    ErrDuplicateRoute       // Route pattern already exists
    ErrCrossBackendOperation // Operation spans backends (legacy)
)
```

## Testing

```go
import "testing"
import "github.com/absfs/memfs"

func TestRouting(t *testing.T) {
    backend1, _ := memfs.NewFS()
    backend2, _ := memfs.NewFS()

    fs, _ := switchfs.New(
        switchfs.WithRoute("/b1/*", backend1),
        switchfs.WithRoute("/b2/*", backend2),
    )

    // Test routing
    fs.Create("/b1/file.txt")  // Goes to backend1
    fs.Create("/b2/file.txt")  // Goes to backend2
}
```

## Dependencies

```
github.com/absfs/absfs                    - Core filesystem interfaces
github.com/bmatcuk/doublestar/v4         - Glob pattern matching
```

### Optional Composition Partners

```
github.com/absfs/osfs      - Local filesystem
github.com/absfs/memfs     - In-memory filesystem
github.com/absfs/s3fs      - Amazon S3 backend
github.com/absfs/cachefs   - Caching layer
github.com/absfs/retryfs   - Retry logic with exponential backoff
github.com/absfs/metricsfs - Prometheus and OpenTelemetry metrics
github.com/absfs/compressfs - Transparent compression
github.com/absfs/encryptfs  - Transparent encryption
github.com/absfs/rofs       - Read-only wrapper
github.com/absfs/lockfs     - Thread-safe wrapper
github.com/absfs/unionfs    - Union/overlay filesystem
github.com/absfs/permfs     - Permission and ACL enforcement
```

## Contributing

Contributions welcome! Please ensure:
- All tests pass (`go test ./...`)
- Code follows Go conventions
- New features include tests and documentation
- Focus on routing-specific functionality (compose for other features)

## License

MIT License - see LICENSE file
