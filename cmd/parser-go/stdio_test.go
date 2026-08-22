package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/parse"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
)

func TestDiagnosticUsesEngineProtocolFields(t *testing.T) {
	delta, err := parse.Parse(protocol.ParseRequest{ProjectName: "demo", Language: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Diagnostics) != 1 {
		t.Fatalf("diagnostics=%#v", delta.Diagnostics)
	}
	payload, err := json.Marshal(delta.Diagnostics[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, legacy := range [][]byte{[]byte(`"severity"`), []byte(`"attributes"`)} {
		if bytes.Contains(payload, legacy) {
			t.Fatalf("legacy diagnostic field in %s", payload)
		}
	}
	for _, required := range [][]byte{[]byte(`"level":"ERROR"`), []byte(`"code":"request.projectRoot.required"`), []byte(`"message":"projectRoot is required"`), []byte(`"details":{}`)} {
		if !bytes.Contains(payload, required) {
			t.Fatalf("missing standard diagnostic field %s in %s", required, payload)
		}
	}
}

func TestParseDemoModuleHasStructureAndRels(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../../testdata/module")
	root, _ = filepath.Abs(root)

	delta, err := parse.Parse(protocol.ParseRequest{
		ProjectName: "demo",
		Language:    "go",
		ProjectRoot: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Packages) == 0 {
		t.Fatal("packages empty")
	}
	if len(delta.Units) == 0 {
		t.Fatal("units empty")
	}
	if len(delta.Functions) == 0 {
		t.Fatal("functions empty")
	}
	// required fields for engine validator
	for _, p := range delta.Packages {
		if p.ID == "" || p.QualifiedName == "" || p.ProjectFilePath == "" || p.ProjectName == "" || p.Language == "" {
			t.Fatalf("package incomplete: %+v", p)
		}
	}
	for _, u := range delta.Units {
		if u.ID == "" || u.QualifiedName == "" || u.ProjectFilePath == "" {
			t.Fatalf("unit incomplete: %+v", u)
		}
	}
	for _, f := range delta.Functions {
		if f.ID == "" || f.QualifiedName == "" || f.ProjectFilePath == "" {
			t.Fatalf("function incomplete: %+v", f)
		}
	}
	relTypes := map[string]int{}
	for _, r := range delta.Relationships {
		if r.RelationshipType == "" || r.FromNodeID == "" || r.ToNodeID == "" || r.ID == "" {
			t.Fatalf("rel incomplete: %+v", r)
		}
		if r.Language == "" || r.ProjectName == "" {
			t.Fatalf("rel meta missing: %+v", r)
		}
		relTypes[r.RelationshipType]++
	}
	if relTypes[protocol.RelPackageToUnit] == 0 {
		t.Fatalf("missing PACKAGE_TO_UNIT: %v", relTypes)
	}
	if relTypes[protocol.RelUnitToFunction] == 0 {
		t.Fatalf("missing UNIT_TO_FUNCTION: %v", relTypes)
	}
	if relTypes[protocol.RelCalls] == 0 {
		t.Fatalf("missing CALLS: %v", relTypes)
	}
	if _, err := json.Marshal(delta); err != nil {
		t.Fatal(err)
	}
}

func TestParseWithRuleSourcesProducesEndpoints(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	ex := filepath.Join(filepath.Dir(file), "../../../static-extract-go/examples/conformance/http-handlefunc")
	ex, _ = filepath.Abs(ex)
	ser, err := os.ReadFile(filepath.Join(ex, "rule.ser"))
	if err != nil {
		t.Fatal(err)
	}
	delta, err := parse.Parse(protocol.ParseRequest{
		ProjectName: "demo",
		Language:    "go",
		ProjectRoot: ex,
		RuleSources: []string{string(ser)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Endpoints) < 2 {
		t.Fatalf("endpoints: %d", len(delta.Endpoints))
	}
	for _, ep := range delta.Endpoints {
		if ep["id"] == nil || ep["endpointKind"] == nil {
			t.Fatalf("endpoint incomplete: %v", ep)
		}
	}
}
