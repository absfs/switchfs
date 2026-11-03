package switchfs

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/absfs/absfs"
)

// wsRoute adds the C4-style route for one workspace.
func wsRoute(sfs *SwitchFS, name string, backend absfs.FileSystem) error {
	return sfs.AddRoute(`^/`+regexp.QuoteMeta(name)+`(/|$)`, backend,
		WithPatternType(PatternRegex),
		WithPriority(100),
		WithRewriter(StripPrefix("/"+name)))
}

func TestDynamic_AddWorkspaceWhileServing(t *testing.T) {
	root := mustMemFS(t)
	sfs, err := New(WithDefault(root))
	if err != nil {
		t.Fatal(err)
	}

	// Before the route exists, /new goes to the default backend.
	if _, err := sfs.Stat("/new/f"); !os.IsNotExist(err) {
		t.Fatalf("Stat before route = %v", err)
	}

	ws := mustMemFS(t)
	writeFile(t, ws, "/f", "inside")
	if err := root.Mkdir("/new", 0755); err != nil {
		t.Fatal(err)
	}
	if err := wsRoute(sfs, "new", ws); err != nil {
		t.Fatal(err)
	}

	// The next operation sees it.
	if got := readFile(t, sfs, "/new/f"); got != "inside" {
		t.Fatalf("after AddRoute = %q", got)
	}

	// A file opened through the route stays usable after the route goes.
	f, err := sfs.OpenFile("/new/g", os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if err := sfs.RemoveRoute(`^/new(/|$)`, PatternRegex); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("still"); err != nil {
		t.Fatalf("write after RemoveRoute: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, ws, "/g"); got != "still" {
		t.Fatalf("backend /g = %q", got)
	}

	// New operations fall back to the default backend again.
	if _, err := sfs.Stat("/new/f"); !os.IsNotExist(err) {
		t.Fatalf("Stat after RemoveRoute = %v", err)
	}
}

// TestDynamic_ConcurrentRouteChanges adds and removes routes while other
// goroutines do file operations, for the race detector. Each worker owns its
// backend, so any race reported is in switchfs, not in memfs.
func TestDynamic_ConcurrentRouteChanges(t *testing.T) {
	root := mustMemFS(t)
	sfs, err := New(WithDefault(root))
	if err != nil {
		t.Fatal(err)
	}

	const workers = 4
	const churners = 2
	iterations := 200
	if testing.Short() {
		iterations = 50
	}

	// Stable workspaces, each with its own backend and worker.
	for i := 0; i < workers; i++ {
		name := fmt.Sprintf("w%d", i)
		if err := root.Mkdir("/"+name, 0755); err != nil {
			t.Fatal(err)
		}
		if err := wsRoute(sfs, name, mustMemFS(t)); err != nil {
			t.Fatal(err)
		}
	}

	// A read-only workspace for the working directory to move into. Both
	// it and the root hold an "f", so relative reads always find one.
	writeFile(t, root, "/f", "root")
	if err := root.Mkdir("/ro", 0755); err != nil {
		t.Fatal(err)
	}
	ro := mustMemFS(t)
	writeFile(t, ro, "/f", "ro")
	if err := wsRoute(sfs, "ro", ro); err != nil {
		t.Fatal(err)
	}

	// Churned workspaces: backends populated up front and only read after.
	churnBackends := make([]absfs.FileSystem, churners)
	for i := range churnBackends {
		b := mustMemFS(t)
		writeFile(t, b, "/marker", fmt.Sprintf("c%d", i))
		churnBackends[i] = b
	}

	var bounded, open sync.WaitGroup // bounded: workers and churners; open: run until stop
	var failures atomic.Int64
	fail := func(format string, args ...any) {
		failures.Add(1)
		t.Errorf(format, args...)
	}
	stop := make(chan struct{})

	for i := 0; i < workers; i++ {
		bounded.Add(1)
		go func(i int) {
			defer bounded.Done()
			base := fmt.Sprintf("/w%d", i)
			for n := 0; n < iterations; n++ {
				p := fmt.Sprintf("%s/f%d", base, n%8)
				f, err := sfs.Create(p)
				if err != nil {
					fail("Create(%s): %v", p, err)
					return
				}
				if _, err := f.WriteString(p); err != nil {
					fail("Write(%s): %v", p, err)
				}
				f.Close()
				data, err := sfs.ReadFile(p)
				if err != nil || string(data) != p {
					fail("ReadFile(%s) = %q, %v", p, data, err)
				}
				if _, err := sfs.Stat(p); err != nil {
					fail("Stat(%s): %v", p, err)
				}
				if _, err := sfs.Lstat(p); err != nil {
					fail("Lstat(%s): %v", p, err)
				}
				if _, err := sfs.ReadDir(base); err != nil {
					fail("ReadDir(%s): %v", base, err)
				}
				if err := sfs.Rename(p, p+".r"); err != nil {
					fail("Rename(%s): %v", p, err)
				}
				if err := sfs.Remove(p + ".r"); err != nil {
					fail("Remove(%s): %v", p, err)
				}
			}
		}(i)
	}

	// Route churn: add and remove the churned workspaces over and over.
	for c := 0; c < churners; c++ {
		bounded.Add(1)
		go func(c int) {
			defer bounded.Done()
			name := fmt.Sprintf("c%d", c)
			pattern := `^/` + name + `(/|$)`
			for n := 0; n < iterations; n++ {
				if err := wsRoute(sfs, name, churnBackends[c]); err != nil {
					fail("AddRoute(%s): %v", name, err)
					return
				}
				_ = sfs.Router().Routes()
				if err := sfs.RemoveRoute(pattern, PatternRegex); err != nil {
					fail("RemoveRoute(%s): %v", name, err)
					return
				}
			}
		}(c)
	}

	// Readers of churned paths: either routed (marker found) or not (the
	// default has no such file). Anything else is a routing bug.
	for c := 0; c < churners; c++ {
		open.Add(1)
		go func(c int) {
			defer open.Done()
			p := fmt.Sprintf("/c%d/marker", c)
			want := fmt.Sprintf("c%d", c)
			for {
				select {
				case <-stop:
					return
				default:
				}
				data, err := sfs.ReadFile(p)
				switch {
				case err == nil && string(data) != want:
					fail("ReadFile(%s) = %q", p, data)
				case err != nil && !os.IsNotExist(err):
					fail("ReadFile(%s): %v", p, err)
				}
				if _, err := sfs.Stat(p); err != nil && !os.IsNotExist(err) {
					fail("Stat(%s): %v", p, err)
				}
			}
		}(c)
	}

	// Working directory changes alongside.
	open.Add(1)
	go func() {
		defer open.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := sfs.Chdir("/ro"); err != nil {
				fail("Chdir: %v", err)
			}
			if err := sfs.Chdir("/"); err != nil {
				fail("Chdir(/): %v", err)
			}
		}
	}()

	// Relative-path readers race the Chdir goroutine for the working
	// directory: each operation sees "/" or "/ro", never anything else.
	for r := 0; r < 2; r++ {
		open.Add(1)
		go func() {
			defer open.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if wd, err := sfs.Getwd(); err != nil || (wd != "/" && wd != "/ro") {
					fail("Getwd = %q, %v", wd, err)
				}
				data, err := sfs.ReadFile("f")
				if err != nil || (string(data) != "root" && string(data) != "ro") {
					fail("ReadFile(f) = %q, %v", data, err)
				}
				if _, err := sfs.Stat("f"); err != nil {
					fail("Stat(f): %v", err)
				}
				if f, err := sfs.Open("./f"); err != nil {
					fail("Open(./f): %v", err)
				} else {
					f.Close()
				}
			}
		}()
	}

	// Once the workers and churners finish, stop the open-ended goroutines.
	bounded.Wait()
	close(stop)
	open.Wait()

	if failures.Load() > 0 {
		return
	}
	if routes := sfs.Router().Routes(); len(routes) != workers+1 {
		t.Fatalf("routes left = %d, want %d", len(routes), workers+1)
	}
}

// TestRouter_ConcurrentLookupAndChange hammers the default router directly:
// lookups in one goroutine, adds and removes in another, in tight loops, so
// the race detector sees the two overlap on every run.
func TestRouter_ConcurrentLookupAndChange(t *testing.T) {
	r := NewRouter()
	m := mustMemFS(t)
	if err := r.AddRoute(Route{Pattern: "/stable", Backend: m}); err != nil {
		t.Fatal(err)
	}
	const n = 2000
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < n; i++ {
			if rt, err := r.RouteWithInfo("/stable/x", nil); err != nil || rt.Pattern != "/stable" {
				t.Errorf("RouteWithInfo = %v, %v", rt, err)
				return
			}
			_, _ = r.RouteWithInfo("/churn/x", nil)
			_, _ = r.Route("/churn/x")
			_ = r.Routes()
		}
	}()
	go func() {
		defer wg.Done()
		tr := r.(TypedRouteRemover)
		for i := 0; i < n; i++ {
			if err := r.AddRoute(Route{Pattern: "/churn", Backend: m, Priority: i % 3}); err != nil {
				t.Errorf("AddRoute: %v", err)
				return
			}
			var err error
			if i%2 == 0 {
				err = r.RemoveRoute("/churn")
			} else {
				err = tr.RemoveRouteOfType("/churn", PatternPrefix)
			}
			if err != nil {
				t.Errorf("remove: %v", err)
				return
			}
		}
	}()
	wg.Wait()
}

func TestRemoveRoute_ByType(t *testing.T) {
	m1, m2 := mustMemFS(t), mustMemFS(t)
	sfs, err := New(
		WithRoute("/same", m1, WithPatternType(PatternPrefix), WithPriority(10)),
		WithRoute("/same", m2, WithPatternType(PatternRegex), WithPriority(5)),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Removing the regex route leaves the higher-priority prefix route.
	if err := sfs.RemoveRoute("/same", PatternRegex); err != nil {
		t.Fatal(err)
	}
	routes := sfs.Router().Routes()
	if len(routes) != 1 || routes[0].Type != PatternPrefix {
		t.Fatalf("routes = %+v", routes)
	}
	if err := sfs.RemoveRoute("/same", PatternRegex); err != ErrNoRoute {
		t.Fatalf("second remove = %v", err)
	}
	if err := sfs.RemoveRoute("/same", PatternPrefix); err != nil {
		t.Fatal(err)
	}

	// Router.RemoveRoute keeps its pattern-only behaviour.
	r := sfs.Router()
	r.AddRoute(Route{Pattern: "/p", Backend: m1, Type: PatternGlob, Priority: 1})
	r.AddRoute(Route{Pattern: "/p", Backend: m2, Type: PatternPrefix, Priority: 2})
	if err := r.RemoveRoute("/p"); err != nil {
		t.Fatal(err)
	}
	routes = r.Routes()
	if len(routes) != 1 || routes[0].Type != PatternGlob {
		t.Fatalf("pattern-only remove took the wrong route: %+v", routes)
	}
}

// plainRouter wraps the default router but hides RemoveRouteOfType, like a
// third-party Router would.
type plainRouter struct{ inner Router }

func (p *plainRouter) AddRoute(r Route) error                      { return p.inner.AddRoute(r) }
func (p *plainRouter) RemoveRoute(pattern string) error            { return p.inner.RemoveRoute(pattern) }
func (p *plainRouter) Route(path string) (absfs.FileSystem, error) { return p.inner.Route(path) }
func (p *plainRouter) RouteWithInfo(path string, info os.FileInfo) (*Route, error) {
	return p.inner.RouteWithInfo(path, info)
}
func (p *plainRouter) Routes() []Route { return p.inner.Routes() }

func TestRemoveRoute_CustomRouterFallback(t *testing.T) {
	sfs, err := New(WithRouter(&plainRouter{inner: NewRouter()}))
	if err != nil {
		t.Fatal(err)
	}
	m := mustMemFS(t)
	if err := sfs.AddRoute("/x", m); err != nil {
		t.Fatal(err)
	}
	if err := sfs.RemoveRoute("/x", PatternGlob); err != ErrNoRoute {
		t.Fatalf("wrong type = %v, want ErrNoRoute", err)
	}
	if err := sfs.RemoveRoute("/x", PatternPrefix); err != nil {
		t.Fatal(err)
	}
	if n := len(sfs.Router().Routes()); n != 0 {
		t.Fatalf("routes left = %d", n)
	}
}

// TestRemoveRoute_CustomRouterAmbiguous: with a router that can only remove
// by pattern, RemoveRoute must not take a same-pattern route of another type.
func TestRemoveRoute_CustomRouterAmbiguous(t *testing.T) {
	sfs, err := New(WithRouter(&plainRouter{inner: NewRouter()}))
	if err != nil {
		t.Fatal(err)
	}
	m1, m2 := mustMemFS(t), mustMemFS(t)
	if err := sfs.AddRoute("/same", m1, WithPriority(10)); err != nil {
		t.Fatal(err)
	}
	if err := sfs.AddRoute("/same", m2, WithPatternType(PatternRegex), WithPriority(5)); err != nil {
		t.Fatal(err)
	}
	if err := sfs.AddRoute("/unrelated", m2, WithPatternType(PatternGlob)); err != nil {
		t.Fatal(err)
	}

	// Either type is ambiguous: removing by pattern could take the other.
	for _, typ := range []PatternType{PatternRegex, PatternPrefix} {
		if err := sfs.RemoveRoute("/same", typ); err != ErrAmbiguousRoute {
			t.Fatalf("RemoveRoute(%v) = %v, want ErrAmbiguousRoute", typ, err)
		}
		if n := len(sfs.Router().Routes()); n != 3 {
			t.Fatalf("routes after refused remove = %d, want 3", n)
		}
	}
	// A type no route has is still ErrNoRoute.
	if err := sfs.RemoveRoute("/same", PatternGlob); err != ErrNoRoute {
		t.Fatalf("RemoveRoute(glob) = %v, want ErrNoRoute", err)
	}

	// Once the pattern is unambiguous again, the remove goes through and
	// takes the route that was asked for.
	if err := sfs.Router().RemoveRoute("/same"); err != nil { // the prefix route
		t.Fatal(err)
	}
	if err := sfs.RemoveRoute("/same", PatternRegex); err != nil {
		t.Fatal(err)
	}
	if routes := sfs.Router().Routes(); len(routes) != 1 || routes[0].Pattern != "/unrelated" {
		t.Fatalf("routes left = %+v", routes)
	}
}

func TestAddRoute_Errors(t *testing.T) {
	sfs, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if err := sfs.AddRoute("/x", nil); err != ErrNilBackend {
		t.Fatalf("nil backend = %v", err)
	}
	bad := errors.New("bad option")
	if err := sfs.AddRoute("/x", mustMemFS(t), func(*Route) error { return bad }); err != bad {
		t.Fatalf("failing option = %v", err)
	}
	if err := sfs.AddRoute("[", mustMemFS(t), WithPatternType(PatternRegex)); err != ErrInvalidPattern {
		t.Fatalf("bad regex = %v", err)
	}
}

func TestRouter_RouteWithInfoReturnsCopy(t *testing.T) {
	r := NewRouter()
	m := mustMemFS(t)
	if err := r.AddRoute(Route{Pattern: "/a", Backend: m}); err != nil {
		t.Fatal(err)
	}
	got, err := r.RouteWithInfo("/a/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	got.Pattern = "/changed"
	if r.Routes()[0].Pattern != "/a" {
		t.Fatal("RouteWithInfo handed out the router's own route")
	}
}

func TestRouter_EqualPrioritiesKeepInsertionOrder(t *testing.T) {
	r := NewRouter()
	first, second := mustMemFS(t), mustMemFS(t)
	r.AddRoute(Route{Pattern: "/", Backend: first})
	r.AddRoute(Route{Pattern: "/x", Backend: second})
	got, err := r.Route("/x/y")
	if err != nil {
		t.Fatal(err)
	}
	if got != absfs.FileSystem(first) {
		t.Fatal("equal-priority routes should match in the order added")
	}
}
