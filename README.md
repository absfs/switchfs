# switchfs

[![Go Reference](https://pkg.go.dev/badge/github.com/absfs/switchfs.svg)](https://pkg.go.dev/github.com/absfs/switchfs)
[![Go Report Card](https://goreportcard.com/badge/github.com/absfs/switchfs)](https://goreportcard.com/report/github.com/absfs/switchfs)
[![CI](https://github.com/absfs/switchfs/actions/workflows/ci.yml/badge.svg)](https://github.com/absfs/switchfs/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**One filesystem tree, many backends, chosen by path.**

`switchfs` is an [absfs](https://github.com/absfs/absfs) filesystem that sends
each operation to a backend filesystem picked by the path: `/hot/...` to
memory, `/archive/...` to disk, `/<workspace>/...` to that workspace's own
overlay. It implements `absfs.FileSystem` and `absfs.SymlinkFileSystem`, so the
composed tree can be served over NFS with
[absnfs](https://github.com/absfs/absnfs), wrapped by other absfs layers, or
used directly from Go.

It does one thing, routing, and leaves caching, overlays, permissions and the
rest to other absfs filesystems you compose with it.

## Contents

- [Install](#install)
- [Quick start](#quick-start)
- [Routing](#routing)
- [Path rewriting](#path-rewriting)
- [What callers see](#what-callers-see)
- [Symlinks](#symlinks)
- [Trailing slashes](#trailing-slashes)
- [Changing routes while serving](#changing-routes-while-serving)
- [Example: one NFS export, a workspace per directory](#example-one-nfs-export-a-workspace-per-directory)
- [Listing the parents of routes](#listing-the-parents-of-routes)
- [Renames across backends](#renames-across-backends)
- [Caveats](#caveats)
- [Testing](#testing)

## Install

```sh
go get github.com/absfs/switchfs
```

Requires Go 1.21 or later.

## Quick start

```go
package main

import (
	"fmt"

	"github.com/absfs/memfs"
	"github.com/absfs/switchfs"
)

func main() {
	hot, _ := memfs.NewFS()
	cold, _ := memfs.NewFS()

	fs, _ := switchfs.New(
		// Anything no route matches goes to cold, unchanged.
		switchfs.WithDefault(cold),
		// /hot and everything below it goes to hot, which sees /hot as its root.
		switchfs.WithRoute("/hot", hot,
			switchfs.WithPriority(100),
			switchfs.WithRewriter(switchfs.StripPrefix("/hot"))),
	)

	f, _ := fs.Create("/hot/session.json") // hot stores /session.json
	f.Close()
	f, _ = fs.Create("/report.txt") // cold stores /report.txt
	f.Close()

	_, err := hot.Stat("/session.json")
	fmt.Println(err == nil) // true
	_, err = cold.Stat("/report.txt")
	fmt.Println(err == nil) // true
}
```

## Routing

Each operation resolves its path and goes to the first route that matches:

1. A relative path is joined to the SwitchFS working directory (`Chdir`,
   `Getwd`), and the result is cleaned with `path.Clean`. Routes only ever see
   clean absolute paths, so `/ws/../etc` is routed as `/etc`.
2. Routes are tried in priority order, highest first. Routes of equal priority
   are tried in the order they were added.
3. If the route has a `Rewriter`, the path is rewritten for the backend
   ([below](#path-rewriting)).
4. A path that matches no route goes, unchanged, to the default backend
   (`WithDefault`). With no default, the operation returns `ErrNoRoute`.

Names follow `os` where cleaning could change their meaning:

- An empty name fails with an `*os.PathError` wrapping `ENOENT` (a
  `*os.LinkError` for `Rename` and `Symlink`), as in `os`. It never means the
  working directory; use `"."` for that.
- `RemoveAll("")` returns nil and does nothing, as `os.RemoveAll` does.
  `RemoveAll` of a name whose last element is `.` or `..` fails with `EINVAL`
  rather than removing the directory that cleaning would make of it.
- A trailing slash requires a directory, as in `os`: `Stat("file.txt/")`
  fails with not-a-directory on a regular file, and `Mkdir("dir/")` works.
  Cleaning drops the slash before routing, so SwitchFS remembers it and
  applies the rule itself; see [Trailing slashes](#trailing-slashes).

Three pattern types:

| Type                      | Example      | Matches                                                        |
| ------------------------- | ------------ | -------------------------------------------------------------- |
| `PatternPrefix` (default) | `/data`      | `/data`, `/data/x/y`; not `/database` (whole components only)  |
| `PatternGlob`             | `**/*.log`   | doublestar globs; a pattern without `/` also matches basenames |
| `PatternRegex`            | `^/ws(/\|$)` | Go regular expressions against the clean path                  |

```go
fs, _ := switchfs.New(
	switchfs.WithDefault(disk),
	switchfs.WithRoute("/cache", mem, switchfs.WithPriority(100)),
	switchfs.WithRoute("**/*.tmp", scratch,
		switchfs.WithPatternType(switchfs.PatternGlob), switchfs.WithPriority(50)),
	switchfs.WithRoute(`^/user/[0-9]+(/|$)`, users,
		switchfs.WithPatternType(switchfs.PatternRegex), switchfs.WithPriority(10)),
)
```

A route may also carry a `Condition` (`MinSize`, `MaxSize`, `SizeRange`,
`OlderThan`, `NewerThan`, `ModifiedBetween`, `DirectoriesOnly`, `FilesOnly`,
combined with `And`, `Or`, `Not`, or your own `RouteCondition`). Operations
evaluate it with the path and a **nil** `os.FileInfo`, because the file's
backend isn't known until routing is done. The built-in metadata conditions
treat a nil info as a match, so during operations they only filter when you
call `Router().RouteWithInfo` yourself with real metadata. `Not(...)` of a
metadata condition is therefore false for every operation, so such a route
never matches one. A path-based custom condition works everywhere.

## Path rewriting

A route's `Rewriter` maps the path a caller uses to the path its backend
stores. It is applied to every path-taking method: `OpenFile`, `Open`,
`Create`, `Mkdir`, `MkdirAll`, `Remove`, `RemoveAll`, `Rename` (both paths,
including cross-backend moves), `Stat`, `Lstat`, `Chmod`, `Chtimes`, `Chown`,
`Lchown`, `Truncate`, `ReadDir`, `ReadFile`, `Readlink`, `Symlink` (the link
path), `Sub` and `Chdir`.

| Rewriter                        | `/ws/a/b` becomes              |
| ------------------------------- | ------------------------------ |
| `StripPrefix("/ws")`            | `/a/b` (and `/ws` becomes `/`) |
| `AddPrefix("/srv")`             | `/srv/ws/a/b`                  |
| `ReplacePrefix("/ws", "/data")` | `/data/a/b`                    |
| `RegexRewrite(pattern, repl)`   | whatever the regexp makes      |
| `StaticMapping(map)`            | the mapped path, if listed     |
| `ChainRewriters(r1, r2, ...)`   | each applied in turn           |

```go
// A backend that keeps /data/... under /storage/...
fs, _ := switchfs.New(
	switchfs.WithRoute("/data", backend,
		switchfs.WithRewriter(switchfs.ReplacePrefix("/data", "/storage"))),
)
fs.Create("/data/report.csv") // backend creates /storage/report.csv
```

Edge cases:

- A rewrite that yields `""` becomes `"/"`. That makes `StripPrefix("/ws")` on
  a `/ws` route map the route's own directory to the backend's root.
- Any other result goes to the backend as is. Rewriters should return absolute
  paths; give `StripPrefix` its prefix without a trailing slash.
- `Remove`, `RemoveAll` and `Rename` of a route's own root act on the
  backend's root.

## What callers see

Rewriting never leaks backend paths:

- A `*os.PathError` from a backend is returned with `Path` set to the name the
  caller passed (relative names stay relative, as with `os`). The wrapped error
  is kept, so `errors.Is`, `os.IsNotExist` and friends work unchanged.
- `Rename` and `Symlink` return `*os.LinkError` with the caller's paths.
- A file opened through a rewriting route returns the caller's name from
  `Name()`, and its `Stat()`, like `SwitchFS.Stat` and `Lstat`, reports the
  base name of the virtual path, so the root of a stripped route is called
  `ws`, not `/`. Files whose backend name already matches are returned as they
  are.
- `ErrNoRoute` is returned unwrapped, and errors that aren't `*os.PathError`
  pass through unchanged, so existing `err == ...` checks keep working.

## Symlinks

`SwitchFS` implements `absfs.SymlinkFileSystem`. `Lstat`, `Lchown`, `Readlink`
and `Symlink` route and rewrite the link's own path like everything else.

| Backend                      | `Lstat`              | `Lchown`, `Readlink`                              | `Symlink`                                         |
| ---------------------------- | -------------------- | ------------------------------------------------- | ------------------------------------------------- |
| implements `absfs.SymLinker` | backend `Lstat`      | backend method                                    | backend method                                    |
| no symlink support           | falls back to `Stat` | `*os.PathError` wrapping `ErrSymlinkNotSupported` | `*os.LinkError` wrapping `ErrSymlinkNotSupported` |

`ErrSymlinkNotSupported` also matches `errors.ErrUnsupported`.

**Link targets are opaque.** `Symlink(target, link)` stores `target` exactly as
given, and `Readlink` returns it unchanged; only `link` is rewritten. A
relative target (`../lib/x`) resolves the same way through the switch and
inside the backend. An absolute target is resolved by whoever follows the
link: a backend following it for `Stat` or `Open` looks in its own, rewritten
namespace, while an NFS client looks on the client machine. Prefer relative
targets, and don't expect a link followed by a backend to lead into another
route.

```go
fs.Symlink("v2.txt", "/ws/current")   // backend: /current -> v2.txt
target, _ := fs.Readlink("/ws/current") // "v2.txt"
data, _ := fs.ReadFile("/ws/current")   // contents of /ws/v2.txt
```

## Trailing slashes

A name ending in `/` must name a directory, as it must for `os`. Routing cleans
the slash away, and it can't simply be handed to backends (`memfs.Mkdir("/e/")`
fails), so SwitchFS notes that the caller's name had one and enforces the rule
itself, against the object the cleaned path names in its backend:

- On a regular file, or a symlink to one, `Stat`, `Lstat`, `Open`,
  `OpenFile`, `Chmod`, `Chtimes`, `Chown`, `Lchown`, `Truncate`, `ReadDir`,
  `ReadFile`, `Readlink`, `Remove`, `Rename` (either path) and `Chdir` fail
  with an `*os.PathError` (`*os.LinkError` for `Rename`) wrapping the host's
  not-a-directory error.
- On a directory nothing changes: `Stat("dir/")`, `Mkdir("new/")`,
  `MkdirAll("a/b/")`, `Rename("dir/", "moved/")` and `Chdir("dir/")` work.
- A trailing slash resolves a final symlink, so `Lstat("link/")` describes the
  directory `link` points to and `Lchown("link/")` changes it.
- `RemoveAll` first tries `Remove` of the name, slash and all, as
  `os.RemoveAll` does.

Hosts disagree on the details, and SwitchFS does what `os` does on the host it
runs on, whatever the backend:

| With a trailing slash                      | Linux                                      | macOS                                          | Windows                            |
| ------------------------------------------ | ------------------------------------------ | ---------------------------------------------- | ---------------------------------- |
| not-a-directory error                      | `ENOTDIR`                                  | `ENOTDIR`                                      | `ERROR_DIRECTORY` (267)            |
| `OpenFile` with `O_CREATE`                 | always `EISDIR`                            | `ENOENT` if missing                            | not-a-directory if missing         |
| `Mkdir`, `MkdirAll` on a file              | `EEXIST`                                   | `ENOTDIR`                                      | the backend's exists error         |
| `Remove`, `Rename` from a symlink to a dir | `ENOTDIR`                                  | act on the directory                           | act on the link                    |
| a dangling symlink                         | `ENOENT`; `ENOTDIR` for `Remove`, `Rename` | followed: `ENOENT`; `Mkdir` creates the target | not-a-directory                    |
| `Rename` onto a missing `new/`             | `ENOTDIR` unless the source is a directory | `ENOENT` unless the source is a directory      | slash ignored, except below a file |
| `RemoveAll("file/")`                       | removes the file                           | removes the file                               | not-a-directory                    |
| `Readlink("dir/")`, `Readlink("link/")`    | `EINVAL`                                   | `EINVAL`                                       | reads the link                     |
| `Chown`, `Lchown`                          | checked                                    | checked                                        | not checked                        |

On Windows the not-a-directory error is `syscall.Errno(267)`, which `os`
returns there. `syscall.ENOTDIR` would be wrong on Windows: it is
`ERROR_PATH_NOT_FOUND`, which matches `fs.ErrNotExist`, so a caller testing
`os.IsNotExist` would take a file in the way for a missing one. Code that
checks for not-a-directory portably should do what it would do with `os`
(test for `syscall.ENOTDIR` on Unix and 267 on Windows). Other Unix hosts get
the Linux rules; only Linux, macOS and Windows are compared against `os` in the
tests.

## Changing routes while serving

Routes can be added and removed at any time, from any goroutine:

```go
// Start serving a new workspace.
err := fs.AddRoute(`^/alice(/|$)`, aliceFS,
	switchfs.WithPatternType(switchfs.PatternRegex),
	switchfs.WithPriority(100),
	switchfs.WithRewriter(switchfs.StripPrefix("/alice")))

// Stop serving it.
err = fs.RemoveRoute(`^/alice(/|$)`, switchfs.PatternRegex)
```

- `AddRoute` takes the same options as `WithRoute`. `RemoveRoute` takes the
  pattern **and** its type, so it never removes a same-pattern route of
  another type. (`Router().RemoveRoute(pattern)` is still there and matches the
  pattern alone, removing the highest-priority such route.)
- With a custom `Router` that doesn't implement `TypedRouteRemover`,
  `RemoveRoute` can only remove by pattern. It does so when no route of
  another type shares the pattern, and otherwise returns `ErrAmbiguousRoute`
  and removes nothing.
- Every operation routes against the routes present when it starts. Files
  already open through a route stay usable after it is removed; new
  operations on its paths go wherever the remaining routes send them.
- The default router guards its table with a read-write lock and hands out
  copies, and the test suite runs route churn against concurrent file
  operations under the race detector. A custom `Router` (`WithRouter`) must be
  safe for concurrent use if you change routes while serving.
- An NFS client may cache attributes and directory listings for a few
  seconds, so a removed workspace can linger in a listing until the cache
  expires.

## Example: one NFS export, a workspace per directory

This is the layout of [C4](https://github.com/Avalanche-io/c4)'s NFS workspace
prototype: one NFSv3 export, with `/<workspace>/...` routed to that workspace's union of a
private writable upper over one shared read-only lower. A new workspace is an
empty upper directory, a union, one directory in the root listing and one
route; nothing is copied.

```go
package main

import (
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/absfs/absnfs"
	"github.com/absfs/basefs"
	"github.com/absfs/memfs"
	"github.com/absfs/osfs"
	"github.com/absfs/switchfs"
	"github.com/absfs/unionfs"
)

type server struct {
	mu     sync.Mutex
	fs     *switchfs.SwitchFS
	root   *memfs.FileSystem // lists the workspaces at "/"
	host   *osfs.FileSystem
	lower  *basefs.SymlinkFileSystem // the shared, read-only tree
	uppers string                    // host directory holding each upper
}

// addWorkspace starts serving /<name>/ while the export is mounted.
func (s *server) addWorkspace(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Join(s.uppers, name)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return err
	}
	upper, err := basefs.NewFS(s.host, dir)
	if err != nil {
		return err
	}
	union := unionfs.New(
		unionfs.WithWritableLayer(upper),
		unionfs.WithReadOnlyLayer(s.lower),
	)
	if err := s.root.Mkdir("/"+name, 0o755); err != nil {
		return err
	}
	return s.fs.AddRoute(`^/`+regexp.QuoteMeta(name)+`(/|$)`, union.SymlinkFileSystem(),
		switchfs.WithPatternType(switchfs.PatternRegex),
		switchfs.WithPriority(100),
		switchfs.WithRewriter(switchfs.StripPrefix("/"+name)))
}

func main() {
	host, _ := osfs.NewFS()
	lower, err := basefs.NewFS(host, "/path/to/shared/tree")
	if err != nil {
		log.Fatal(err)
	}
	root, _ := memfs.NewFS()
	sw, _ := switchfs.New(switchfs.WithDefault(root))
	s := &server{fs: sw, root: root, host: host, lower: lower, uppers: "/path/to/uppers"}

	if err := s.addWorkspace("alice"); err != nil {
		log.Fatal(err)
	}

	// SwitchFS is an absfs.SymlinkFileSystem, so absnfs serves it directly.
	nfs, err := absnfs.New(sw, absnfs.ExportOptions{})
	if err != nil {
		log.Fatal(err)
	}
	srv, err := absnfs.NewServer(absnfs.ServerOptions{
		Port: 12049, MountPort: 12049, UseRecordMarking: true,
	})
	if err != nil {
		log.Fatal(err)
	}
	srv.SetHandler(nfs)
	if err := srv.Listen(); err != nil {
		log.Fatal(err)
	}
	select {} // call s.addWorkspace from a control endpoint as needed
}
```

On macOS a normal user can then mount it without root:

```sh
mkdir -p ~/ws
mount_nfs -o vers=3,tcp,port=12049,mountport=12049,noresvport,locallocks localhost:/ ~/ws
ls ~/ws        # alice
```

While experimenting, add `soft,intr,timeo=10,retrans=2` to the mount options:
with the default hard mount, stopping the server while it is mounted hangs
every process that touches the mount until the server returns. Unmount with
`umount ~/ws`.

Notes on this layout:

- The regex `^/alice(/|$)` matches `/alice` and below, not `/alice2`.
  (A `PatternPrefix` route on `/alice` does the same.)
- `StripPrefix` makes each union see its workspace as `/`, so the upper's host
  directory holds exactly the workspace's changes.
- Use relative symlink targets inside workspaces (see [Symlinks](#symlinks)):
  the NFS client resolves absolute targets on the client.
- Following a link from the writable upper to a target in the shared lower
  is the union's job, not the switch's. With unionfs v1.0.0 such a link does
  not resolve when read through the union (with or without switchfs); it
  needs a unionfs release newer than v1.0.0.

## Listing the parents of routes

A directory listing comes from the one backend its path routes to; routes
below it are **not** merged in. With routes `/a` and `/b` and nothing else,
`ReadDir("/")` fails with `ErrNoRoute`. The pattern that works, used above, is
a `memfs` default backend holding one directory per route:

```go
root, _ := memfs.NewFS()
fs, _ := switchfs.New(switchfs.WithDefault(root))
for _, name := range []string{"a", "b"} {
	root.Mkdir("/"+name, 0o755)
	fs.AddRoute("/"+name, backends[name], switchfs.WithRewriter(switchfs.StripPrefix("/"+name)))
}
entries, _ := fs.ReadDir("/") // a, b
```

`Stat("/a")` goes to the `/a` route (its backend's root), so the entry and the
directory agree on being a directory; their other metadata comes from
different backends.

## Renames across backends

`Rename` uses the backend's own `Rename`, with both paths rewritten, whenever
both paths route to the same backend, even through two different routes.
Otherwise it copies the tree into the new backend and removes it from the old
one:

- regular files are streamed, keeping permission bits and (where the backend
  allows) modification times;
- directories are copied recursively;
- symlinks are recreated with the same target, not followed; moving one onto a
  backend without symlink support fails with `ErrSymlinkNotSupported`;
- an existing destination is handled as `rename(2)` handles it: a file or
  symlink replaces a file or symlink there (a symlink at the destination is
  replaced, not written through), and fails with `EISDIR` on a directory; a
  directory replaces only an empty directory, failing with `ENOTEMPTY` or
  `ENOTDIR` otherwise;
- the move is **not atomic**. A failure part way leaves what was already moved
  in place; a source file is removed only after its copy succeeded.

## Caveats

- **Absolute symlink targets aren't rewritten** (by design, see
  [Symlinks](#symlinks)).
- **Listings don't merge routes**; see
  [Listing the parents of routes](#listing-the-parents-of-routes).
- **Conditions see a nil `FileInfo`** during operations; see [Routing](#routing).
- **Cross-backend renames are copies**, not atomic.
- **`Route.Failover` / `WithFailover` are recorded but not used**; every
  operation goes to the route's `Backend`.
- A file returned under a different name than its backend's (any file opened
  through a rewriting route) is a wrapper exposing the `absfs.File` methods
  only; optional interfaces of the backend's file type, such as `Fd()`, are
  hidden.
- `TempDir()` is the virtual path `/tmp` (or `WithTempDir`), routed like any
  other path.
- Trailing slashes follow the host's `os`, not the backend (see
  [Trailing slashes](#trailing-slashes)): a memfs-backed SwitchFS returns
  `EISDIR` for `Create("x/")` on Linux and `ENOENT` on macOS. A trailing
  `/.` or `/..` is still cleaned away, so `Stat("file.txt/.")` succeeds on a
  regular file where `os.Stat` fails.
- `RemoveAll` of a route's own root removes the backend's root contents; with
  `ReplacePrefix` onto a host directory, that is the host directory.

## Testing

```sh
go test -race ./...
```

The tests run the [fstesting](https://github.com/absfs/fstesting) conformance
suite against SwitchFS over memfs (with symlinks), over rewriting routes
(`StripPrefix`, `ReplacePrefix`, a C4-style regex workspace route), and over
osfs both with and without rewriting, plus race-detector tests that add and
remove routes and change the working directory while other goroutines read,
write, rename and list with absolute and relative paths. The suite links to
absolute virtual paths, which by design a backend behind a rewriting route
can't follow, so the rewriting runs repeat its symlink group through a small
test shim that stores those targets as relative links.

## License

MIT, see [LICENSE](LICENSE).
