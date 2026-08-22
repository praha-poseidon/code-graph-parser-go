package parse

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

func TestTaskSessionReusesGoplsAndRelationshipsAreOrderIndependent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gopls fixture uses a POSIX shell")
	}
	root := t.TempDir()
	contractDir := filepath.Join(root, "contract")
	implDir := filepath.Join(root, "impl")
	if err := os.MkdirAll(contractDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(implDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contractFile := filepath.Join(contractDir, "service.go")
	implFile := filepath.Join(implDir, "user.go")
	writeFixture(t, filepath.Join(root, "go.mod"), "module example.com/incremental\n\ngo 1.23\n")
	writeFixture(t, contractFile, "package contract\n\ntype Service interface {\n\tSave() error\n}\n")
	writeFixture(t, implFile, "package impl\n\ntype UserService struct{}\n\nfunc (UserService) Save() error { return nil }\n")

	cacheDir := filepath.Join(root, ".codegraph-cache", "gopls")
	cacheLog := filepath.Join(root, "cache.log")
	processLog := filepath.Join(root, "process.log")
	fakeGopls := filepath.Join(root, "fake-gopls")
	script := fmt.Sprintf(`#!/bin/sh
printf '%%s\n' "$GOPLSCACHE" >> %q
printf 'start\n' >> %q
export CODEGRAPH_FAKE_GOPLS=1
export CODEGRAPH_FAKE_CONTRACT_FILE=%q
export CODEGRAPH_FAKE_IMPL_FILE=%q
exec %q -test.run='^TestGoplsLSPHelper$'
`, cacheLog, processLog, contractFile, implFile, os.Args[0])
	writeFixture(t, fakeGopls, script)
	if err := os.Chmod(fakeGopls, 0o755); err != nil {
		t.Fatal(err)
	}

	options := map[string]any{
		"goplsCommand":  fakeGopls,
		"goplsCacheDir": cacheDir,
		"goplsRequired": true,
	}
	newRequest := func(sourceFile string) protocol.ParseRequest {
		return protocol.ParseRequest{
			ProjectName: "incremental",
			Language:    "go",
			ProjectRoot: root,
			SourceFiles: []string{sourceFile},
			ChangeType:  "SOURCE_MODIFIED",
			Options:     options,
		}
	}
	session, err := OpenTaskSession(context.Background(), newRequest(contractFile))
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	for _, sourceFile := range []string{contractFile, implFile} {
		delta, err := session.Parse(newRequest(sourceFile))
		if err != nil {
			t.Fatal(err)
		}
		if len(delta.Diagnostics) != 0 {
			t.Fatalf("source=%s diagnostics=%#v", sourceFile, delta.Diagnostics)
		}
		assertRelationship(t, delta,
			"unit:example.com/incremental/impl.UserService",
			protocol.RelSatisfies,
			"unit:example.com/incremental/contract.Service")
		assertRelationship(t, delta,
			"fn:example.com/incremental/impl.UserService.Save",
			protocol.RelMethodSatisfies,
			"fn:example.com/incremental/contract.Service.Save")

		// file= incremental loading must not emit the other package's nodes.
		for _, unit := range delta.Units {
			if sourceFile == contractFile && unit.QualifiedName == "example.com/incremental/impl.UserService" {
				t.Fatalf("contract delta unexpectedly loaded implementation unit: %#v", unit)
			}
			if sourceFile == implFile && unit.QualifiedName == "example.com/incremental/contract.Service" {
				t.Fatalf("implementation delta unexpectedly loaded contract unit: %#v", unit)
			}
		}
	}

	cacheUses, err := os.ReadFile(cacheLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(cacheUses); got == "" {
		t.Fatal("fake gopls did not observe GOPLSCACHE")
	}
	for _, line := range splitNonEmptyLines(string(cacheUses)) {
		if line != cacheDir {
			t.Fatalf("GOPLSCACHE=%q, want %q", line, cacheDir)
		}
	}
	processStarts, err := os.ReadFile(processLog)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(splitNonEmptyLines(string(processStarts))); got != 1 {
		t.Fatalf("gopls process starts=%d, want one shared by two file requests", got)
	}
}

func TestGoplsLSPHelper(t *testing.T) {
	if os.Getenv("CODEGRAPH_FAKE_GOPLS") != "1" {
		return
	}
	if err := serveFakeGopls(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func serveFakeGopls(input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	for {
		body, err := readFakeFrame(reader)
		if err != nil {
			return err
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal(body, &message); err != nil {
			return err
		}
		switch message.Method {
		case "initialize":
			if err := writeFakeResponse(output, message.ID, map[string]any{
				"capabilities": map[string]any{"implementationProvider": true},
			}); err != nil {
				return err
			}
		case "textDocument/implementation":
			locations, err := fakeImplementationLocations(message.Params)
			if err != nil {
				return err
			}
			if err := writeFakeResponse(output, message.ID, locations); err != nil {
				return err
			}
		case "shutdown":
			if err := writeFakeResponse(output, message.ID, nil); err != nil {
				return err
			}
		case "exit":
			return nil
		}
	}
}

func fakeImplementationLocations(raw json.RawMessage) ([]map[string]any, error) {
	var params struct {
		TextDocument struct {
			URI string `json:"uri"`
		} `json:"textDocument"`
		Position struct {
			Line int `json:"line"`
		} `json:"position"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	filename := filepath.FromSlash(parsed.Path)
	contractFile := os.Getenv("CODEGRAPH_FAKE_CONTRACT_FILE")
	implFile := os.Getenv("CODEGRAPH_FAKE_IMPL_FILE")
	var target string
	var line, character int
	switch {
	case filename == contractFile && params.Position.Line == 2:
		target, line, character = implFile, 2, 5
	case filename == contractFile && params.Position.Line == 3:
		target, line, character = implFile, 4, 19
	case filename == implFile && params.Position.Line == 2:
		target, line, character = contractFile, 2, 5
	case filename == implFile && params.Position.Line == 4:
		target, line, character = contractFile, 3, 1
	default:
		return []map[string]any{}, nil
	}
	return []map[string]any{{
		"uri": (&url.URL{Scheme: "file", Path: filepath.ToSlash(target)}).String(),
		"range": map[string]any{
			"start": map[string]any{"line": line, "character": character},
			"end":   map[string]any{"line": line, "character": character + 1},
		},
	}}, nil
}

func readFakeFrame(reader *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
		}
	}
	if length < 0 {
		return nil, fmt.Errorf("missing Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	return body, err
}

func writeFakeResponse(output io.Writer, id json.RawMessage, result any) error {
	var decodedID any
	if err := json.Unmarshal(id, &decodedID); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": decodedID, "result": result})
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "Content-Length: %d\r\n\r\n", len(body)); err != nil {
		return err
	}
	_, err = output.Write(body)
	return err
}

func writeFixture(t *testing.T, filename, contents string) {
	t.Helper()
	if err := os.WriteFile(filename, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertRelationship(t *testing.T, delta protocol.GraphDelta, from, relType, to string) {
	t.Helper()
	for _, relationship := range delta.Relationships {
		if relationship.FromNodeID == from && relationship.RelationshipType == relType && relationship.ToNodeID == to {
			return
		}
	}
	t.Fatalf("missing %s --%s--> %s; relationships=%#v", from, relType, to, delta.Relationships)
}

func splitNonEmptyLines(value string) []string {
	var lines []string
	start := 0
	for i := 0; i <= len(value); i++ {
		if i < len(value) && value[i] != '\n' {
			continue
		}
		if line := value[start:i]; line != "" {
			lines = append(lines, line)
		}
		start = i + 1
	}
	return lines
}
