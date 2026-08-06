package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

func TestParseWithRuleSourcesProducesEndpoints(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	// .../cmd/code-graph-parser-go -> repo root -> sibling static-extract-go example
	repo := filepath.Join(filepath.Dir(file), "../..")
	ex := filepath.Join(repo, "../static-extract-go/examples/conformance/http-handlefunc")
	ex, _ = filepath.Abs(ex)
	ser, err := os.ReadFile(filepath.Join(ex, "rule.ser"))
	if err != nil {
		t.Fatal(err)
	}
	req := protocol.ParseRequest{
		ProjectName: "demo",
		Language:    "go",
		ProjectRoot: ex,
		RuleSources: []string{string(ser)},
	}
	delta, err := parse(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Packages) == 0 {
		t.Fatal("packages empty")
	}
	if len(delta.Endpoints) < 2 {
		b, _ := json.MarshalIndent(delta.Endpoints, "", "  ")
		t.Fatalf("endpoints: %s", b)
	}
	// must be valid JSON shape for process protocol
	if _, err := json.Marshal(delta); err != nil {
		t.Fatal(err)
	}
}
