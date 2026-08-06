// Package load wraps go/packages for whole-module AST + types.
package load

import (
	"fmt"
	"path/filepath"

	"golang.org/x/tools/go/packages"
)

type Config struct {
	Dir      string
	Patterns []string
	Tests    bool
}

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
	pkgs, err := packages.Load(&packages.Config{
		Mode:  mode,
		Dir:   abs,
		Tests: cfg.Tests,
	}, patterns...)
	if err != nil {
		return nil, err
	}
	var out []*packages.Package
	for _, p := range pkgs {
		// skip pure test binaries when Tests=false still can appear
		if len(p.Syntax) == 0 {
			continue
		}
		// skip "command-line-arguments" noise
		if p.PkgPath == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("load: no packages with syntax under %s patterns %v", abs, patterns)
	}
	return out, nil
}
