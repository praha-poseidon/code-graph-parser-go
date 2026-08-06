package parse_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/parse"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

func relTypes(delta protocol.GraphDelta) map[string]int {
	m := map[string]int{}
	for _, r := range delta.Relationships {
		m[r.RelationshipType]++
	}
	return m
}

func TestImplementsAndOverrides(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../../testdata/iface")
	root, _ = filepath.Abs(root)
	delta, err := parse.Parse(protocol.ParseRequest{
		ProjectName: "iface",
		Language:    "go",
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := relTypes(delta)
	if rt[protocol.RelImplements] == 0 {
		t.Fatalf("expected IMPLEMENTS, got %v", rt)
	}
	if rt[protocol.RelOverrides] == 0 {
		t.Fatalf("expected OVERRIDES, got %v", rt)
	}
	if rt[protocol.RelExtends] == 0 {
		// LoudPerson embeds Person; Shouter embeds Greeter
		t.Fatalf("expected EXTENDS (embed), got %v", rt)
	}
	// interface methods exist as functions
	foundIfaceMethod := false
	for _, f := range delta.Functions {
		if f.Name == "Greet" && f.QualifiedName != "" {
			foundIfaceMethod = true
			break
		}
	}
	if !foundIfaceMethod {
		t.Fatal("expected interface method function nodes")
	}
}

func TestEndpointToFunction(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	ex := filepath.Join(filepath.Dir(file), "../../../static-extract-go/examples/conformance/http-handlefunc")
	ex, _ = filepath.Abs(ex)
	// read rule via parse package using file
	serPath := filepath.Join(ex, "rule.ser")
	// load via OS in test
	b, err := readFile(serPath)
	if err != nil {
		t.Fatal(err)
	}
	delta, err := parse.Parse(protocol.ParseRequest{
		ProjectName: "http",
		Language:    "go",
		ProjectRoot: ex,
		RuleSources: []string{string(b)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Endpoints) < 2 {
		t.Fatalf("endpoints %d", len(delta.Endpoints))
	}
	rt := relTypes(delta)
	if rt[protocol.RelEndpointToFunc] == 0 {
		t.Fatalf("expected ENDPOINT_TO_FUNCTION, got %v endpoints=%d", rt, len(delta.Endpoints))
	}
}

func TestSourceFilesFilter(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../../testdata/module")
	root, _ = filepath.Abs(root)
	delta, err := parse.Parse(protocol.ParseRequest{
		ProjectName: "demo",
		Language:    "go",
		ProjectRoot: root,
		SourceFiles: []string{filepath.Join(root, "main.go")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Functions) == 0 {
		t.Fatal("expected functions from main.go")
	}
}

// local helper to avoid importing os in every test file style
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
