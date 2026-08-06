// Package parse builds a GraphDelta from a ParseRequest (code-graph process protocol).
//
// Pipeline (aligned with java-jdt processors):
//  1. packages / units / functions + PACKAGE_TO_UNIT / UNIT_TO_FUNCTION
//  2. CALLS (+ placeholder functions for external targets)
//  3. EXTENDS (struct/interface embed)
//  4. IMPLEMENTS (types.Implements)
//  5. OVERRIDES (interface methods + embed shadow)
//  6. endpoints from ruleSources + ENDPOINT_TO_FUNCTION / FUNCTION_TO_ENDPOINT
package parse

import (
	"path/filepath"

	"github.com/praha-poseidon/code-graph-parser-go/internal/load"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

// Parse is the main entry used by the CLI (--stdio and debug modes).
func Parse(req protocol.ParseRequest) (protocol.GraphDelta, error) {
	root := req.ProjectRoot
	if root == "" && len(req.SourceRoots) > 0 {
		root = req.SourceRoots[0]
	}
	if root == "" {
		d := protocol.EmptyDelta(req)
		d.Diagnostics = append(d.Diagnostics, protocol.Diagnostic{
			Level: "ERROR", Message: "projectRoot is required",
		})
		return d, nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return protocol.GraphDelta{}, err
	}
	if req.Language == "" {
		req.Language = "go"
	}
	req.ProjectRoot = abs

	// Always load full module for types; FileAllow filters emitted nodes when sourceFiles set.
	pkgs, err := load.Packages(load.Config{
		Dir:      abs,
		Patterns: []string{"./..."},
		Tests:    false,
	})
	if err != nil {
		d := protocol.EmptyDelta(req)
		d.Scope.ProjectRoot = abs
		d.Diagnostics = append(d.Diagnostics, protocol.Diagnostic{
			Level: "ERROR", Message: err.Error(),
		})
		return d, nil
	}

	c := newContext(req, abs, pkgs)

	collectPackagesUnitsFunctions(c)
	collectCalls(c)
	collectInheritance(c)
	collectImplements(c)
	collectOverrides(c)
	if err := attachEndpoints(c); err != nil {
		c.diag("ERROR", "endpoints: "+err.Error())
	}

	return *c.Delta, nil
}
