# switchfs

Path-based routing filesystem for the AbsFs ecosystem. Route filesystem operations to different backend implementations based on path patterns, enabling hybrid storage architectures with seamless integration.

## Overview

`switchfs` provides intelligent path-based routing to different filesystem backends, allowing you to build sophisticated storage architectures without application-level awareness. Operations are transparently routed to the appropriate backend based on configurable path patterns and glob matching.

**Key Features:**
- Route operations to different backends by path prefix or glob pattern
- Support for multiple simultaneous backends (local, cloud, memory, etc.)
- Transparent operation routing - applications see a unified filesystem
- Configurable routing rules with priority ordering
- Fallback and failover support
- Performance-aware routing decisions
- Move/rename across backend boundaries with automatic data transfer

## Use Cases

### Hybrid Storage
Combine local and cloud storage in a single filesystem view:
- Fast local storage for working files
- Cloud storage for archives and backups
- Automatic routing based on path structure

### Tiered Storage
Implement hot/warm/cold storage tiers:
- Hot: SSD/NVMe for frequently accessed data
- Warm: HDD for regular access
- Cold: Cloud storage for archives

### Local + Cloud Sync
Build Dropbox-like functionality:
- Local cache for immediate access
- Cloud backend for persistence and sharing
- Transparent routing and caching

### Development/Production Separation
Route different paths to different environments:
- `/dev/*` to local development filesystem
- `/prod/*` to production cloud storage
- Test configurations without production impact

### Multi-Cloud Architecture
Route to different cloud providers:
- `/aws/*` to S3-backed filesystem
- `/gcs/*` to Google Cloud Storage
- `/azure/*` to Azure Blob Storage

## Implementation Phases

### Phase 1: Core Routing Infrastructure
- Define Route and Router interfaces
- Implement path pattern matching (prefix, glob, regex)
- Build routing decision engine with priority ordering
- Create basic SwitchFS implementation
- Implement absfs.FileSystem interface with routing
- Add route registration and management

### Phase 2: Advanced Routing Features
- Cross-backend move/rename operations
- Automatic data transfer between backends
- Route wildcards and dynamic patterns
- Conditional routing (size, time, metadata)
- Route aliasing and path rewriting
- Default/fallback backend support

### Phase 3: Performance and Optimization
- Route caching and lookup optimization
- Parallel operation support across backends
- Smart prefetching for predictable patterns
- Backend health monitoring
- Performance metrics and routing decisions
- Lazy backend initialization

### Phase 4: Failover and Reliability
- Backend health checking
- Automatic failover to backup backends
- Retry logic with exponential backoff
- Read-through caching for failed backends
- Route availability monitoring
- Circuit breaker pattern for failing backends

### Phase 5: Advanced Features
- Middleware/interceptor support
- Route-specific operation logging
- Bandwidth throttling per backend
- Access control per route
- Dynamic route reconfiguration
- Route statistics and analytics

## API Design

### Basic Types

```go
// Route defines a routing rule
type Route struct {
    Pattern  string           // Path pattern (prefix, glob, or regex)
    Backend  absfs.FileSystem // Target backend filesystem
    Priority int              // Higher priority routes match first
    Type     PatternType      // Prefix, Glob, or Regex
}

// PatternType defines how patterns are matched
type PatternType int

const (
    PatternPrefix PatternType = iota  // Simple prefix match
    PatternGlob                        // Glob pattern match
    PatternRegex                       // Regular expression match
)

// Router manages routing decisions
type Router interface {
    // Add a routing rule
    AddRoute(route Route) error

    // Remove a routing rule by pattern
    RemoveRoute(pattern string) error

    // Find the backend for a given path
    Route(path string) (absfs.FileSystem, error)

    // List all routes
    Routes() []Route
}

// SwitchFS implements absfs.FileSystem with routing
type SwitchFS struct {
    router  Router
    default absfs.FileSystem // Optional default backend
}
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

// WithPriority sets route priority
func WithPriority(priority int) RouteOption

// WithPatternType sets the pattern matching type
func WithPatternType(pt PatternType) RouteOption

// WithFailover sets a failover backend
func WithFailover(fs absfs.FileSystem) RouteOption
```

## Usage Examples

### Hot/Warm/Cold Storage Tiers

```go
// Create backends
hotFS := memfs.NewFS()           // In-memory for hot data
warmFS := osfs.NewFS("/warm")    // Local SSD for warm data
coldFS := s3fs.NewFS(s3Config)   // S3 for cold data

// Create switch filesystem with tiered routing
fs, err := switchfs.New(
    switchfs.WithRoute("/hot/*", hotFS,
        switchfs.WithPriority(100)),
    switchfs.WithRoute("/warm/*", warmFS,
        switchfs.WithPriority(50)),
    switchfs.WithRoute("/cold/*", coldFS,
        switchfs.WithPriority(10)),
    switchfs.WithDefault(warmFS),
)

// Application code - transparent routing
file, _ := fs.OpenFile("/hot/cache.dat", os.O_RDWR|os.O_CREATE, 0644)
// Automatically routed to memory backend

archive, _ := fs.OpenFile("/cold/archive-2020.tar.gz", os.O_RDONLY, 0)
// Automatically routed to S3 backend
```

### Local + Cloud Hybrid

```go
// Local filesystem for working files
localFS := osfs.NewFS("/home/user/workspace")

// Cloud filesystem for shared/backup
cloudFS := s3fs.NewFS(s3Config)

fs, err := switchfs.New(
    // Working files stay local
    switchfs.WithRoute("/workspace/*", localFS,
        switchfs.WithPriority(100)),

    // Shared files in cloud
    switchfs.WithRoute("/shared/*", cloudFS,
        switchfs.WithPriority(100)),

    // Archives in cloud
    switchfs.WithRoute("/archive/*", cloudFS,
        switchfs.WithPriority(100)),

    // Everything else defaults to local
    switchfs.WithDefault(localFS),
)

// Move file from workspace to archive
// SwitchFS handles cross-backend transfer
fs.Rename("/workspace/old-project.zip", "/archive/old-project.zip")
// Data automatically transferred from local to S3
```

### Multi-Cloud with Failover

```go
// Primary and backup cloud storage
primaryS3 := s3fs.NewFS(primaryConfig)
backupGCS := gcsfs.NewFS(gcsConfig)

fs, err := switchfs.New(
    switchfs.WithRoute("/documents/*", primaryS3,
        switchfs.WithPriority(100),
        switchfs.WithFailover(backupGCS)),

    switchfs.WithRoute("/media/*", primaryS3,
        switchfs.WithPriority(100),
        switchfs.WithFailover(backupGCS)),
)

// Operations automatically failover on backend errors
file, _ := fs.Open("/documents/report.pdf")
// Uses primaryS3, falls back to backupGCS if primary fails
```

### Development/Production Split

```go
devFS := osfs.NewFS("/data/dev")
prodFS := s3fs.NewFS(prodConfig)

mode := os.Getenv("ENV")
var fs absfs.FileSystem

if mode == "production" {
    fs, _ = switchfs.New(
        switchfs.WithRoute("/data/*", prodFS),
    )
} else {
    fs, _ = switchfs.New(
        switchfs.WithRoute("/data/*", devFS),
    )
}

// Same code works in both environments
data, _ := fs.ReadFile("/data/config.json")
```

### Pattern-Based Routing

```go
localFS := osfs.NewFS("/local")
cloudFS := s3fs.NewFS(s3Config)

fs, err := switchfs.New(
    // Large files go to cloud
    switchfs.WithRoute("*.{mp4,mkv,avi}", cloudFS,
        switchfs.WithPatternType(switchfs.PatternGlob),
        switchfs.WithPriority(100)),

    // Temporary files stay local
    switchfs.WithRoute("/tmp/*", localFS,
        switchfs.WithPriority(90)),

    // Cache directories stay local
    switchfs.WithRoute("**/.cache/*", localFS,
        switchfs.WithPatternType(switchfs.PatternGlob),
        switchfs.WithPriority(90)),

    // Default to local
    switchfs.WithDefault(localFS),
)
```

## Performance Considerations

### Routing Decisions
- Route lookup uses prefix trees for O(log n) performance
- Glob patterns compiled once at route registration
- Route cache for frequently accessed paths
- Priority-ordered evaluation stops at first match

### Cross-Backend Operations
- Move/rename across backends requires data copy
- Large file transfers use chunked streaming
- Parallel transfers for directory operations
- Progress callbacks for long-running transfers

### Optimization Strategies
- Cache routing decisions for repeated paths
- Lazy initialization of backends
- Connection pooling for network backends
- Prefetch hints for predictable access patterns

## Failover Scenarios

### Backend Unavailability
```go
// Primary backend fails
err := primaryFS.Open("/file.txt")
// Returns: backend unavailable

// SwitchFS automatically tries failover
file, err := switchFS.Open("/file.txt")
// Successfully retrieves from failover backend
```

### Partial Backend Failure
```go
// Some operations succeed, others fail
// SwitchFS can implement per-operation failover
err := primaryFS.Stat("/might-not-exist.txt")
// Tries failover only if primary is healthy but file missing
```

### Recovery and Fallback
- Monitor backend health continuously
- Automatic re-routing when primary recovers
- Circuit breaker prevents cascading failures
- Configurable retry logic with backoff

## Thread Safety

All operations are thread-safe:
- Route registration/removal uses read-write locks
- Concurrent operations routed independently
- Backend-specific concurrency handled by backends
- No shared state between routed operations

## Error Handling

```go
// ErrNoRoute returned when no route matches
var ErrNoRoute = errors.New("no route found for path")

// ErrAllBackendsFailed when primary and failover fail
var ErrAllBackendsFailed = errors.New("all backends failed")

// ErrCrossBackendOperation for unsupported operations
var ErrCrossBackendOperation = errors.New("operation spans multiple backends")
```

## Testing Strategy

- Unit tests for routing logic
- Integration tests with multiple mock backends
- Performance benchmarks for routing overhead
- Failover scenario testing
- Cross-backend operation tests
- Concurrent access stress tests

## Dependencies

```
github.com/absfs/absfs    - Core filesystem interfaces
```

Optional integrations:
- `github.com/absfs/osfs` - Local filesystem
- `github.com/absfs/memfs` - In-memory filesystem
- `github.com/absfs/s3fs` - S3 backend
- Any absfs.FileSystem implementation

## Contributing

Contributions welcome! Please ensure:
- All tests pass
- Code follows Go conventions
- New features include tests and documentation
- Performance impact is measured

## License

MIT License - see LICENSE file
