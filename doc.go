// Package switchfs is an absfs filesystem that routes each operation to one
// of several backend filesystems by its path.
//
// A SwitchFS holds a list of routes, each a pattern (prefix, glob or regular
// expression) and a backend, plus an optional default backend for paths no
// route matches. It implements absfs.FileSystem and absfs.SymlinkFileSystem,
// so it can be handed to anything that serves or consumes an absfs tree,
// such as an NFS server (absnfs) or another wrapper.
//
// # Routing
//
// Each operation resolves its path, picks the first matching route and
// passes the (possibly rewritten) path to that route's backend:
//
//  1. A relative path is joined to the SwitchFS working directory (see
//     Chdir), and the result is cleaned with path.Clean. Routing sees only
//     clean absolute paths, so "/ws/../etc" is routed as "/etc".
//  2. Routes are tried in priority order, highest first; routes of equal
//     priority are tried in the order they were added. A prefix pattern
//     matches whole path components: "/data" matches "/data" and
//     "/data/x" but not "/database". A route's Condition, if any, is
//     evaluated with the path and a nil os.FileInfo (the built-in
//     metadata conditions treat a nil info as a match).
//  3. If the route has a Rewriter, the path is rewritten. A rewrite that
//     yields "" becomes "/", so StripPrefix("/ws") maps "/ws" to the
//     backend's root. Any other result is passed to the backend as is, so
//     rewriters should produce absolute paths.
//  4. With no matching route the path goes, unrewritten, to the default
//     backend; with no default the operation returns ErrNoRoute, unwrapped.
//
// # What callers see
//
// Callers only ever see their own (virtual) paths:
//
//   - A *os.PathError from a backend is returned with Path set to the name
//     the caller passed; Op and the wrapped error are kept, so errors.Is
//     and os.IsNotExist work as before. Rename and Symlink return
//     *os.LinkError values with the caller's paths. Errors of other types
//     are returned unchanged.
//   - A file opened through a rewriting route reports the caller's name
//     from Name, and its Stat, like SwitchFS.Stat and Lstat, reports the
//     base name of the virtual path. (Without the rename, the root of a
//     route stripped to "/" would be called "/".) Files whose backend name
//     already matches are returned unwrapped.
//
// # Symlinks
//
// Lstat, Lchown, Readlink and Symlink route and rewrite the link's own path
// like every other operation. When the backend doesn't implement
// absfs.SymLinker, Lstat falls back to Stat, and the other three fail with
// an error wrapping ErrSymlinkNotSupported.
//
// Link targets are opaque: Symlink stores the target exactly as given and
// Readlink returns it unchanged. A relative target resolves the same way in
// the virtual tree and inside the backend. An absolute target is resolved
// by whoever follows the link: a backend following it for Stat or Open
// looks it up in the backend's own (rewritten) namespace, while an NFS
// client looks it up on the client. Prefer relative targets, and expect a
// link never to lead from one route into another when a backend follows it.
//
// # Renames across backends
//
// When both paths of a Rename route to the same backend, its own Rename is
// used with both rewritten paths, even across two routes. Otherwise the
// tree is copied into the new backend (keeping permission bits, and
// modification times where the backend allows; symlinks are recreated, not
// followed) and then removed from the old one. That copy is not atomic: a
// failure part way leaves what was moved in place, and the source is only
// removed once its copy succeeded.
//
// # Dynamic routes
//
// Routes may be added and removed while the filesystem is serving, with
// SwitchFS.AddRoute and SwitchFS.RemoveRoute or through Router. The default
// router is safe for concurrent use: every operation routes against the
// routes present when it starts, and files already open stay usable after
// their route is removed. SwitchFS.RemoveRoute takes the pattern type as
// well as the pattern; Router.RemoveRoute matches the pattern alone.
//
// # Listing the parents of routes
//
// A directory listing comes from the one backend its path routes to; routes
// below it are not merged in. To make "/" list the workspaces of routes
// such as "/alpha" and "/beta", use a memfs as the default backend and
// create a directory in it for each route, as the package example does.
//
// # Not implemented
//
// Route.Failover and WithFailover are recorded on the route but not yet
// used: operations go to Backend only.
package switchfs
