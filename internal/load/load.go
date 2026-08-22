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

	roots, err := moduleRoots(abs, patterns)
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
		rootPatterns := patternsForRoot(root, roots, patterns)
		if len(rootPatterns) == 0 {
			continue
		}
		pkgs, err := packages.Load(&packages.Config{
			Mode:  mode,
			Dir:   root,
			Tests: cfg.Tests,
		}, rootPatterns...)
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

// patternsForRoot keeps file= queries attached to the module that contains
// the file. This matters for go.work and shallow multi-module repositories:
// asking every module to load the same absolute file causes avoidable errors
// and defeats incremental package loading.
func patternsForRoot(root string, roots, patterns []string) []string {
	var filePatterns []string
	var otherPatterns []string
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "file=") {
			filename := strings.TrimPrefix(pattern, "file=")
			if ownerRootForFile(roots, filename) == root {
				filePatterns = append(filePatterns, pattern)
			}
			continue
		}
		otherPatterns = append(otherPatterns, pattern)
	}
	if len(filePatterns) > 0 {
		return filePatterns
	}
	if hasFilePattern(patterns) {
		return nil
	}
	return otherPatterns
}

func ownerRootForFile(roots []string, filename string) string {
	var owner string
	for _, root := range roots {
		if pathWithin(root, filename) && len(root) > len(owner) {
			owner = root
		}
	}
	return owner
}

func hasFilePattern(patterns []string) bool {
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, "file=") {
			return true
		}
	}
	return false
}

func pathWithin(root, filename string) bool {
	rel, err := filepath.Rel(root, filename)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func isTestPackagePath(pkgPath string) bool {
	return strings.HasSuffix(pkgPath, ".test")
}

// moduleRoots returns directories that contain a go.mod to load.
// Prefer go.work "use" entries when present.
func moduleRoots(projectRoot string, patterns []string) ([]string, error) {
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
	// For incremental file= requests, the nearest module owns the file. Check
	// this before the root go.mod because nested modules intentionally shadow it.
	if roots := moduleRootsForFiles(projectRoot, patterns); len(roots) > 0 {
		return roots, nil
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

func moduleRootsForFiles(projectRoot string, patterns []string) []string {
	seen := map[string]bool{}
	var roots []string
	for _, pattern := range patterns {
		if !strings.HasPrefix(pattern, "file=") {
			continue
		}
		filename := strings.TrimPrefix(pattern, "file=")
		if !filepath.IsAbs(filename) {
			filename = filepath.Join(projectRoot, filename)
		}
		dir := filepath.Dir(filepath.Clean(filename))
		for pathWithin(projectRoot, dir) {
			if st, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !st.IsDir() {
				if !seen[dir] {
					seen[dir] = true
					roots = append(roots, dir)
				}
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return roots
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
