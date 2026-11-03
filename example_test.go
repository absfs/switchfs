package switchfs_test

import (
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/absfs/absfs"
	"github.com/absfs/memfs"
	"github.com/absfs/switchfs"
)

// addWorkspace routes /<name>/... to backend, which sees the workspace's
// files at its own root, and lists <name> in the root directory.
func addWorkspace(sfs *switchfs.SwitchFS, root absfs.FileSystem, name string, backend absfs.FileSystem) error {
	if err := root.Mkdir("/"+name, 0755); err != nil {
		return err
	}
	return sfs.AddRoute(`^/`+regexp.QuoteMeta(name)+`(/|$)`, backend,
		switchfs.WithPatternType(switchfs.PatternRegex),
		switchfs.WithPriority(100),
		switchfs.WithRewriter(switchfs.StripPrefix("/"+name)))
}

// One tree with a directory per workspace, each served by its own backend.
// The default backend is a memfs that holds only the workspace directories,
// so listing "/" shows them; each route strips its prefix, so a backend
// sees its workspace as "/".
func Example() {
	root, _ := memfs.NewFS()
	sfs, _ := switchfs.New(switchfs.WithDefault(root))

	alpha, _ := memfs.NewFS()
	beta, _ := memfs.NewFS()
	addWorkspace(sfs, root, "alpha", alpha)
	addWorkspace(sfs, root, "beta", beta)

	f, _ := sfs.Create("/alpha/notes.txt")
	f.Write([]byte("hello"))
	f.Close()

	entries, _ := sfs.ReadDir("/")
	for _, e := range entries {
		fmt.Println("/" + e.Name())
	}

	// The backend stores the file at its own root.
	data, _ := alpha.ReadFile("/notes.txt")
	fmt.Println(string(data))

	// Errors and file names use the path the caller gave.
	_, err := sfs.Stat("/beta/missing")
	var pe *os.PathError
	errors.As(err, &pe)
	fmt.Println(pe.Path, os.IsNotExist(err))
	// Output:
	// /alpha
	// /beta
	// hello
	// /beta/missing true
}

// A Rewriter maps the path a caller uses to the path the backend stores.
func ExampleWithRewriter() {
	backend, _ := memfs.NewFS()
	backend.MkdirAll("/storage", 0755)

	sfs, _ := switchfs.New(
		switchfs.WithRoute("/data", backend,
			switchfs.WithRewriter(switchfs.ReplacePrefix("/data", "/storage"))),
	)

	f, _ := sfs.Create("/data/report.csv")
	fmt.Println(f.Name())
	f.Close()

	_, err := backend.Stat("/storage/report.csv")
	fmt.Println(err == nil)
	// Output:
	// /data/report.csv
	// true
}

// Routes can be added and removed while the filesystem is in use, for
// example to start serving a new workspace.
func ExampleSwitchFS_AddRoute() {
	root, _ := memfs.NewFS()
	sfs, _ := switchfs.New(switchfs.WithDefault(root))

	ws, _ := memfs.NewFS()
	f, _ := ws.Create("/README")
	f.Close()

	sfs.AddRoute("/ws", ws, switchfs.WithRewriter(switchfs.StripPrefix("/ws")))
	_, err := sfs.Stat("/ws/README")
	fmt.Println("after AddRoute:", err == nil)

	sfs.RemoveRoute("/ws", switchfs.PatternPrefix)
	_, err = sfs.Stat("/ws/README")
	fmt.Println("after RemoveRoute:", err == nil)
	// Output:
	// after AddRoute: true
	// after RemoveRoute: false
}

// Symlinks are created in the backend the link's path routes to. The target
// is stored as given; a relative target resolves the same way through the
// switch and inside the backend.
func ExampleSwitchFS_Symlink() {
	ws, _ := memfs.NewFS()
	sfs, _ := switchfs.New(
		switchfs.WithRoute("/ws", ws, switchfs.WithRewriter(switchfs.StripPrefix("/ws"))),
	)

	f, _ := sfs.Create("/ws/v2.txt")
	f.Write([]byte("version two"))
	f.Close()

	sfs.Symlink("v2.txt", "/ws/current")

	target, _ := sfs.Readlink("/ws/current")
	data, _ := sfs.ReadFile("/ws/current")
	fmt.Println(target)
	fmt.Println(string(data))
	// Output:
	// v2.txt
	// version two
}
