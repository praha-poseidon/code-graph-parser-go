package parse_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/parse"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

func TestEmbedEmitsExtends(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../../testdata/embed")
	root, _ = filepath.Abs(root)
	delta, err := parse.Parse(protocol.ParseRequest{
		ProjectName: "embed",
		Language:    "go",
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range delta.Relationships {
		if r.RelationshipType == protocol.RelExtends {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected EXTENDS, rels=%d", len(delta.Relationships))
	}
}
