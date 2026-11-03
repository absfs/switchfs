# Changelog

All notable changes to switchfs are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project
uses [Semantic Versioning](https://semver.org/).

## [Unreleased]

Planned as v1.1.0. The behaviour changes below (under Fixed and Changed) make
switchfs do what v1.0.0 documented or what `os` does; they are treated as bug
fixes, not breaking changes, and no exported API is removed or changed.
Callers that relied on the old behaviour should check `Chdir` (now fails on a
missing or non-directory target, and with `ErrNoRoute` on an unrouted path when
there is no default backend), `Rename` errors (now `*os.LinkError`), prefix
matching (`/data` no longer matches `/database`), conditions (now evaluated
during operations), empty names (now `ENOENT`), names with a trailing slash
(now not-a-directory on a file), and cross-backend renames onto existing
destinations.

### Fixed

- **A route's `Rewriter` is now applied.** Every path-taking method (`OpenFile`,
  `Open`, `Create`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename` with
  both paths and cross-backend moves, `Stat`, `Chmod`, `Chtimes`, `Chown`,
  `Truncate`, `ReadDir`, `ReadFile`, `Sub`, `Chdir`) routes through the rewrite.
  In v1.0.0 rewriters were ignored and every backend received the full virtual
  path. A rewrite that yields `""` becomes `"/"`.
- Relative paths are resolved against the SwitchFS working directory before
  routing, and every path is cleaned, so `/ws/../x` routes as `/x`. In v1.0.0
  relative paths went to the backend unresolved and `Chdir` had no effect on
  routing.
- Data race: `RouteWithInfo` returned a pointer into the router's route slice,
  which a concurrent `AddRoute` (in-place sort) or `RemoveRoute` (in-place
  shift) mutated. It now returns a copy, and route changes build a new slice.
- Prefix patterns match whole path components: `/data` matches `/data` and
  `/data/x`, no longer `/database`.
- Cross-backend moves recreate symlinks instead of copying their targets, keep
  a file's permission bits and (where the backend allows) its modification time,
  and close the source before removing it.
- Cross-backend moves treat an existing destination as `rename(2)` does: a file
  or symlink replaces a non-directory (a symlink there is replaced, not written
  through) and fails with `EISDIR` on a directory; a directory replaces only an
  empty directory and fails with `ENOTEMPTY` or `ENOTDIR` otherwise. Previously
  a file was written through a destination symlink and a directory was merged
  into a non-empty one.
- An empty name fails with `ENOENT` (as in `os`) in every method instead of
  reaching the backend; `RemoveAll("")` is a no-op returning nil, and
  `RemoveAll` of a name ending in `.` or `..` fails with `EINVAL`, so cleaning
  can never turn it into a removal of the working directory.
- **A trailing slash requires a directory**, as in `os`. Cleaning used to drop
  it, so `Stat("file/")`, `Open`, `Lstat`, `Remove`, `Rename` and the rest
  acted on a regular file. SwitchFS now remembers the slash and fails with the
  host's not-a-directory error on a file or a symlink to one (`ENOTDIR` on
  Unix; on Windows `ERROR_DIRECTORY`, as `os` returns there, which unlike
  `syscall.ENOTDIR` doesn't match `fs.ErrNotExist`). The slash resolves a
  final symlink (`Lstat("link/")` describes the directory), `Mkdir("dir/")`
  and `MkdirAll` work as before, and the details that differ by host
  (`OpenFile` with `O_CREATE`, `Mkdir` on a file, `Remove` and `Rename` of a
  symlink to a directory, a slash on a `Rename` destination) follow `os` on
  the host. The slash overrides `O_NOFOLLOW`, a symlink loop fails with
  `ELOOP` (on Linux `ENOTDIR` for `Remove` and `Rename`, as there) instead of
  the link being removed or renamed, and links are resolved through symlinked
  parent directories before a target's `..`, so a slash never redirects a
  `Remove`, `Rename` or `Mkdir` to a different directory than `os` would use.
  A table-driven test compares every method with `os` on the host.

### Added

- `SwitchFS` implements `absfs.SymlinkFileSystem`: `Lstat`, `Lchown`,
  `Readlink` and `Symlink`, routed and rewritten like every other method. On a
  backend without symlink support `Lstat` falls back to `Stat` and the others
  return an error wrapping the new `ErrSymlinkNotSupported` (which matches
  `errors.ErrUnsupported`). Link targets are stored and returned verbatim.
- Callers see their own paths: `*os.PathError` values carry the caller's name,
  `Rename` and `Symlink` return `*os.LinkError` with the caller's paths, files
  report the caller's name from `Name()`, and `FileInfo.Name()` is the base of
  the virtual path (so a route root stripped to `/` is still called by its
  name).
- `SwitchFS.AddRoute(pattern, backend, opts...)` and
  `SwitchFS.RemoveRoute(pattern, type)` for changing routes while serving, and
  the `TypedRouteRemover` interface (`RemoveRouteOfType`), implemented by the
  default router, to remove a route by pattern and type together. With a
  custom router lacking it, `RemoveRoute` returns the new `ErrAmbiguousRoute`
  instead of removing by pattern when routes of other types share the pattern.
- Package documentation (`doc.go`), runnable examples, and fstesting runs over
  rewriting routes and an osfs backend.

### Changed

- Operations now evaluate a route's `Condition` (with a nil `os.FileInfo`), as
  `RouteWithInfo` always did; in v1.0.0 operations used `Route`, which ignores
  conditions. Built-in metadata conditions treat a nil info as a match, but
  `Not(...)` of one never matches during an operation.
- `Chdir` checks that the target exists and is a directory, like `os.Chdir`,
  and leaves the working directory unchanged when it isn't.
- `Sub` serves the subtree through the SwitchFS itself, so routes below `dir`
  keep working inside the returned `fs.FS`.
- Routes of equal priority are tried in the order they were added (the sort is
  now stable).
- `Rename` errors from a backend are reported as `*os.LinkError`, as the
  `absfs.Filer` contract specifies.

### Unchanged on purpose

- Operations on a path with no route and no default still return `ErrNoRoute`
  unwrapped, and backend errors that aren't `*os.PathError` pass through as is,
  so `err == ErrNoRoute` and similar comparisons keep working.
- `Router.RemoveRoute(pattern)` still matches the pattern alone.
- `Route.Failover` is still recorded but not used, so `ErrAllBackendsFailed`
  and `ErrCrossBackendOperation` are never returned; their docs now say so.

## [1.0.0]

- First stable release for absfs 1.0.
