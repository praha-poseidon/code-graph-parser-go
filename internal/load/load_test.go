package load_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/load"
	"golang.org/x/tools/go/packages"
)

func TestModuleRootsFromGoWork(t *testing.T) {
	dir := t.TempDir()
	// root module
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/root\n\ngo 1.22\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n")
	// child module
	mustWrite(t, filepath.Join(dir, "server", "go.mod"), "module example.com/server\n\ngo 1.22\n")
	mustWrite(t, filepath.Join(dir, "server", "s.go"), "package server\nfunc F() {}\n")
	mustWrite(t, filepath.Join(dir, "go.work"), "go 1.22\n\nuse (\n\t.\n\t./server\n)\n")

	pkgs, err := load.Packages(load.Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, p := range pkgs {
		paths[p.PkgPath] = true
	}
	if !paths["example.com/root"] {
		t.Fatalf("missing root package, got %v", paths)
	}
	if !paths["example.com/server"] {
		t.Fatalf("missing server package, got %v", paths)
	}
}

func TestIncrementalGoWorkFileUsesMostSpecificModule(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/root\n\ngo 1.22\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package root\n")
	serverFile := filepath.Join(dir, "server", "internal", "server.go")
	mustWrite(t, filepath.Join(dir, "server", "go.mod"), "module example.com/server\n\ngo 1.22\n")
	mustWrite(t, serverFile, "package internal\nfunc Serve() {}\n")
	mustWrite(t, filepath.Join(dir, "go.work"), "go 1.22\n\nuse (\n\t.\n\t./server\n)\n")

	pkgs, err := load.Packages(load.Config{Dir: dir, Patterns: []string{"file=" + serverFile}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgPath != "example.com/server/internal" {
		t.Fatalf("expected only the most specific go.work module, got %#v", packagePaths(pkgs))
	}
}

func TestIncrementalFileUsesNearestDeepModule(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "go.mod"), "module example.com/root\n\ngo 1.22\n")
	mustWrite(t, filepath.Join(dir, "main.go"), "package root\n")
	deepFile := filepath.Join(dir, "services", "users", "internal", "user.go")
	mustWrite(t, filepath.Join(dir, "services", "users", "go.mod"), "module example.com/users\n\ngo 1.22\n")
	mustWrite(t, deepFile, "package internal\nfunc Save() {}\n")

	pkgs, err := load.Packages(load.Config{Dir: dir, Patterns: []string{"file=" + deepFile}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 1 || pkgs[0].PkgPath != "example.com/users/internal" {
		t.Fatalf("expected nearest nested module package, got %#v", packagePaths(pkgs))
	}
}

func packagePaths(pkgs []*packages.Package) []string {
	paths := make([]string, 0, len(pkgs))
	for _, pkg := range pkgs {
		paths = append(paths, pkg.PkgPath)
	}
	return paths
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
