package graph_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ast"
	"github.com/praha-poseidon/code-graph-parser-go/internal/graph"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

func TestBuildDemoModule(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../../testdata/module")
	pkgs, err := ast.Load(ast.LoadConfig{Dir: root, Patterns: []string{"./..."}})
	if err != nil {
		t.Fatal(err)
	}
	delta := graph.Build(protocol.ParseRequest{ProjectName: "demo", ProjectRoot: root}, pkgs)
	if len(delta.Packages) == 0 {
		t.Fatal("no packages")
	}
	if len(delta.Functions) == 0 {
		t.Fatal("no functions")
	}
	if len(delta.Units) == 0 {
		t.Fatal("no units")
	}
}
