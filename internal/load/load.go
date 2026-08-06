// Package load wraps go/packages for whole-module / workspace AST + types.
package load

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Config struct {
	Dir      string
	Patterns []string
	Tests    bool
}

// Packages loads Go packages under Dir.
// If Dir is a go.work workspace, every listed module is loaded (./...) and merged.
// Single-module projects load Dir with ./... as before.
func Packages(cfg Config) ([]*packages.Package, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("load: project root Dir is required")
	}
	abs, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, err
	}
	patterns := cfg.Patterns
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	roots, err := moduleRoots(abs)
	if err != nil {
		return nil, err
	}

	mode := packages.NeedName |
		packages.NeedFiles |
		packages.NeedCompiledGoFiles |
		packages.NeedSyntax |
		packages.NeedTypes |
		packages.NeedTypesInfo |
		packages.NeedTypesSizes |
		packages.NeedModule |
		packages.NeedImports |
		packages.NeedDeps

	seen := map[string]bool{}
	var out []*packages.Package
	var loadErrs []string

	for _, root := range roots {
		pkgs, err := packages.Load(&packages.Config{
			Mode:  mode,
			Dir:   root,
			Tests: cfg.Tests,
		}, patterns...)
		if err != nil {
			loadErrs = append(loadErrs, fmt.Sprintf("%s: %v", root, err))
			continue
		}
		for _, p := range pkgs {
			if len(p.Syntax) == 0 || p.PkgPath == "" {
				continue
			}
			if isTestPackagePath(p.PkgPath) {
				continue
			}
			// de-dupe by PkgPath (workspace modules can overlap)
			if seen[p.PkgPath] {
				continue
			}
			seen[p.PkgPath] = true
			out = append(out, p)
		}
	}

	if len(out) == 0 {
		msg := fmt.Sprintf("load: no packages with syntax under %s (modules=%v)", abs, roots)
		if len(loadErrs) > 0 {
			msg += "; errors: " + strings.Join(loadErrs, "; ")
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return out, nil
}

func isTestPackagePath(pkgPath string) bool {
	return strings.HasSuffix(pkgPath, ".test")
}

// moduleRoots returns directories that contain a go.mod to load.
// Prefer go.work "use" entries when present.
func moduleRoots(projectRoot string) ([]string, error) {
	work := filepath.Join(projectRoot, "go.work")
	if st, err := os.Stat(work); err == nil && !st.IsDir() {
		uses, err := parseGoWorkUses(work, projectRoot)
		if err != nil {
			return nil, err
		}
		if len(uses) > 0 {
			return uses, nil
		}
	}
	// single module or workspace without uses
	if _, err := os.Stat(filepath.Join(projectRoot, "go.mod")); err == nil {
		return []string{projectRoot}, nil
	}
	// fallback: scan one level for go.mod (shallow monorepo)
	entries, err := os.ReadDir(projectRoot)
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sub := filepath.Join(projectRoot, e.Name())
		if _, err := os.Stat(filepath.Join(sub, "go.mod")); err == nil {
			roots = append(roots, sub)
		}
	}
	if len(roots) == 0 {
		return []string{projectRoot}, nil
	}
	return roots, nil
}

func parseGoWorkUses(workFile, projectRoot string) ([]string, error) {
	f, err := os.Open(workFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var uses []string
	inUse := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		if strings.HasPrefix(line, "use ") {
			// use ./foo  OR  use (
			rest := strings.TrimSpace(strings.TrimPrefix(line, "use"))
			if rest == "(" {
				inUse = true
				continue
			}
			// single-line use ./path
			path := strings.Trim(rest, "\"")
			if path != "" && path != "(" {
				uses = append(uses, resolveUse(projectRoot, path))
			}
			continue
		}
		if inUse {
			if line == ")" {
				inUse = false
				continue
			}
			path := strings.Trim(line, "\"")
			if path != "" {
				uses = append(uses, resolveUse(projectRoot, path))
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	// filter missing dirs
	var out []string
	for _, u := range uses {
		if st, err := os.Stat(u); err == nil && st.IsDir() {
			out = append(out, u)
		}
	}
	return out, nil
}

func resolveUse(projectRoot, use string) string {
	use = strings.TrimSpace(use)
	if filepath.IsAbs(use) {
		return filepath.Clean(use)
	}
	return filepath.Clean(filepath.Join(projectRoot, use))
}
