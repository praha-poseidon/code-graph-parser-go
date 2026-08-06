package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ast"
	"github.com/praha-poseidon/code-graph-parser-go/internal/endpoint"
	"github.com/praha-poseidon/code-graph-parser-go/internal/graph"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"github.com/praha-poseidon/static-extract-go/extractapi"
)

func main() {
	stdio := flag.Bool("stdio", false, "read ParseRequest JSON from stdin, write GraphDelta JSON to stdout")
	project := flag.String("project", "", "project root (debug mode without --stdio)")
	rule := flag.String("rule", "", "optional SER file for endpoint extraction (debug)")
	flag.Parse()

	if *stdio {
		if err := runStdio(); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		return
	}
	if *project == "" {
		fmt.Fprintln(os.Stderr, "usage: code-graph-parser-go --stdio | --project <dir> [--rule file.ser]")
		os.Exit(2)
	}
	req := protocol.ParseRequest{
		ProjectName: filepath.Base(*project),
		Language:    "go",
		ProjectRoot: *project,
	}
	if *rule != "" {
		b, err := os.ReadFile(*rule)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		req.RuleSources = []string{string(b)}
	}
	delta, err := parse(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(delta)
}

func runStdio() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	var req protocol.ParseRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return err
	}
	if req.Language == "" {
		req.Language = "go"
	}
	delta, err := parse(req)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(delta)
}

func parse(req protocol.ParseRequest) (protocol.GraphDelta, error) {
	root := req.ProjectRoot
	if root == "" && len(req.SourceRoots) > 0 {
		root = req.SourceRoots[0]
	}
	if root != "" {
		if a, err := filepath.Abs(root); err == nil {
			root = a
			req.ProjectRoot = root
		}
	}
	patterns := []string{"./..."}
	pkgs, err := ast.Load(ast.LoadConfig{Dir: root, Patterns: patterns})
	if err != nil {
		return protocol.GraphDelta{}, err
	}
	delta := graph.Build(req, pkgs)

	if len(req.RuleSources) > 0 {
		facts, err := extractapi.Run(extractapi.Request{
			ProjectRoot:    root,
			Packages:       pkgs,
			RuleSources:    req.RuleSources,
			ExternalValues: req.ExternalValues,
		})
		if err != nil {
			delta.Diagnostics = append(delta.Diagnostics, protocol.Diagnostic{
				Severity: "ERROR",
				Message:  "static-extract: " + err.Error(),
			})
		} else {
			delta.Endpoints = endpoint.ToGraphEndpoints(req.ProjectName, facts)
		}
	}
	return delta, nil
}
