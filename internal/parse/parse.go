// Package parse builds one incremental GraphDelta for the engine process protocol.
//
// Responsibility boundary:
//   - Input:  ParseRequest from the engine (often only changed sourceFiles).
//   - Output: nodes/relationships/endpoints discovered for this parse.
//   - NOT responsible for graph storage, merge policy, or deletions.
//     deletedNodeIds / deletedRelationshipIds stay empty; the engine applies
//     SOURCE_DELETED and cascade itself (same as parser-js).
//
// Pipeline:
//  1. packages / units / functions + PACKAGE_TO_UNIT / UNIT_TO_FUNCTION
//  2. CALLS (+ placeholder functions for external targets)
//  3. EXTENDS (struct/interface embed)
//  4. IMPLEMENTS (types.Implements)
//  5. OVERRIDES (interface methods + embed shadow)
//  6. endpoints from ruleSources + ENDPOINT_TO_FUNCTION / FUNCTION_TO_ENDPOINT
package parse

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/praha-poseidon/code-graph-parser-go/internal/gopls"
	"github.com/praha-poseidon/code-graph-parser-go/internal/load"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

// Parse is the main entry used by the CLI (--stdio and debug modes).
func Parse(req protocol.ParseRequest) (protocol.GraphDelta, error) {
	return parse(req, nil, nil)
}

// TaskSession reuses one gopls workspace while the engine processes the
// changed files of one cloned project sequentially.
type TaskSession struct {
	root     string
	gopls    *gopls.Session
	goplsErr error
	closed   bool
}

// OpenTaskSession creates disposable task state. It never persists state
// outside the cloned project and must be closed before that project is removed.
func OpenTaskSession(ctx context.Context, req protocol.ParseRequest) (*TaskSession, error) {
	root, err := requestRoot(req)
	if err != nil {
		return nil, err
	}
	session := &TaskSession{root: root}
	if len(req.SourceFiles) == 0 || !optionBool(req.Options, "goplsEnabled", true) {
		return session, nil
	}
	client := gopls.Client{
		Command:  optionString(req.Options, "goplsCommand", os.Getenv("CODEGRAPH_GOPLS_COMMAND")),
		Root:     root,
		CacheDir: optionString(req.Options, "goplsCacheDir", filepath.Join(root, ".codegraph-cache", "gopls")),
	}
	if err := client.Available(); err != nil {
		session.goplsErr = err
		return session, nil
	}
	session.gopls, session.goplsErr = client.Open(ctx)
	return session, nil
}

// Parse processes one file request using this task's workspace process.
func (s *TaskSession) Parse(req protocol.ParseRequest) (protocol.GraphDelta, error) {
	if s.closed {
		return protocol.GraphDelta{}, errors.New("parser task session is closed")
	}
	root, err := requestRoot(req)
	if err != nil {
		return protocol.GraphDelta{}, err
	}
	if root != s.root {
		return protocol.GraphDelta{}, errors.New("parser task session cannot switch projectRoot")
	}
	return parse(req, s.gopls, s.goplsErr)
}

// Close releases gopls and all in-memory workspace state.
func (s *TaskSession) Close() {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.gopls != nil {
		s.gopls.Close()
		s.gopls = nil
	}
}

func parse(req protocol.ParseRequest, goplsSession *gopls.Session, goplsErr error) (protocol.GraphDelta, error) {
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

	// Incremental requests load only the package(s) containing the changed files
	// plus their required dependencies. A full request still loads ./.... Global
	// implementation lookup for incremental requests is supplied by gopls below.
	patterns := []string{"./..."}
	if len(req.SourceFiles) > 0 {
		patterns = make([]string, 0, len(req.SourceFiles))
		for _, sourceFile := range req.SourceFiles {
			if sourceFile == "" {
				continue
			}
			if !filepath.IsAbs(sourceFile) {
				sourceFile = filepath.Join(abs, sourceFile)
			}
			if sourceFile, err = filepath.Abs(sourceFile); err != nil {
				return protocol.GraphDelta{}, err
			}
			patterns = append(patterns, "file="+sourceFile)
		}
	}

	pkgs, err := load.Packages(load.Config{
		Dir:      abs,
		Patterns: patterns,
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
	c.Gopls = goplsSession
	c.GoplsErr = goplsErr

	collectPackagesUnitsFunctions(c)
	collectCalls(c)
	collectInheritance(c)
	collectImplements(c)
	collectOverrides(c)
	collectGoplsImplementations(c)
	if err := attachEndpoints(c); err != nil {
		c.diag("ERROR", "endpoints: "+err.Error())
	}

	return *c.Delta, nil
}

func requestRoot(req protocol.ParseRequest) (string, error) {
	root := req.ProjectRoot
	if root == "" && len(req.SourceRoots) > 0 {
		root = req.SourceRoots[0]
	}
	if root == "" {
		return "", errors.New("projectRoot is required")
	}
	return filepath.Abs(root)
}
