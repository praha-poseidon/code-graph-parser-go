// code-graph-parser-go is the Go language process parser for code-graph-engine.
//
// Java does not parse Go. This CLI is invoked by code-graph-parser-process:
//
//	stdin  → ParseRequest JSON
//	stdout → GraphDelta JSON
//
// Same role as code-graph-parser-js.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/praha-poseidon/code-graph-parser-go/internal/parse"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

func main() {
	stdio := flag.Bool("stdio", false, "process protocol: ParseRequest on stdin → GraphDelta on stdout")
	stdioStream := flag.Bool("stdio-stream", false, "stream protocol: multiple ParseRequest/GraphDelta JSON lines")
	project := flag.String("project", "", "debug: parse module at path (pretty JSON to stdout)")
	rule := flag.String("rule", "", "debug: optional SER file for endpoints")
	flag.Parse()

	if *stdioStream {
		if err := runStdioStream(); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		return
	}

	if *stdio {
		if err := runStdio(); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		return
	}

	if *project == "" {
		fmt.Fprintf(os.Stderr, `Usage:
  code-graph-parser-go --stdio
      Read one ParseRequest JSON from stdin, write one GraphDelta JSON to stdout.
      Used by code-graph-engine / code-graph-parser-process.

  code-graph-parser-go --stdio-stream
      Keep one task-local parser/gopls process alive for multiple requests.
      Input and output are newline-delimited compact JSON.

  code-graph-parser-go --project <moduleRoot> [--rule file.ser]
      Local debug (pretty-print GraphDelta).

Engine example:
  -Dcodegraph.parser.process.languages=go
  -Dcodegraph.parser.process.go.command="/path/to/code-graph-parser-go --stdio-stream"
`)
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
	delta, err := parse.Parse(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(delta); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func runStdio() error {
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return err
	}
	var req protocol.ParseRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return fmt.Errorf("invalid ParseRequest JSON: %w", err)
	}
	if req.Language == "" {
		req.Language = "go"
	}
	delta, err := parse.Parse(req)
	if err != nil {
		return err
	}
	// compact JSON for process adapter
	return json.NewEncoder(os.Stdout).Encode(delta)
}

func runStdioStream() error {
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	var session *parse.TaskSession
	defer func() {
		if session != nil {
			session.Close()
		}
	}()

	for {
		var req protocol.ParseRequest
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("invalid streaming ParseRequest JSON: %w", err)
		}
		if req.Language == "" {
			req.Language = "go"
		}
		if session == nil {
			var err error
			session, err = parse.OpenTaskSession(context.Background(), req)
			if err != nil {
				return err
			}
		}
		delta, err := session.Parse(req)
		if err != nil {
			return err
		}
		if err := encoder.Encode(delta); err != nil {
			return err
		}
	}
}
