package endpoint_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ast"
	"github.com/praha-poseidon/code-graph-parser-go/internal/endpoint"
	"github.com/praha-poseidon/static-extract-go/extractapi"
)

func TestEndpointsFromHandleFunc(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../../../static-extract-go/examples/conformance/http-handlefunc")
	ser, err := os.ReadFile(filepath.Join(root, "rule.ser"))
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := ast.Load(ast.LoadConfig{Dir: root, Patterns: []string{"./input"}})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := extractapi.Run(extractapi.Request{
		ProjectRoot: root,
		Packages:    pkgs,
		RuleSources: []string{string(ser)},
	})
	if err != nil {
		t.Fatal(err)
	}
	eps := endpoint.ToGraphEndpoints("demo", facts)
	if len(eps) < 2 {
		t.Fatalf("want >=2 endpoints, got %d", len(eps))
	}
	if eps[0]["endpointKind"] != "http" {
		t.Fatalf("kind: %v", eps[0]["endpointKind"])
	}
}
