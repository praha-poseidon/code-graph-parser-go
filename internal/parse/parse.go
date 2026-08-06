// Package parse builds a GraphDelta from a ParseRequest (code-graph process protocol).
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

	// 1) nodes + PACKAGE_TO_UNIT / UNIT_TO_FUNCTION
	collectPackagesUnitsFunctions(c)
	// 2) CALLS
	collectCalls(c)
	// 3) EXTENDS (embed)
	collectInheritance(c)
	// 4) optional endpoints from ruleSources (separate module)
	if err := attachEndpoints(c); err != nil {
		c.diag("endpoints: " + err.Error())
	}

	return *c.Delta, nil
}
