package parse_test

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
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
	rels := map[string]protocol.CodeRelationship{}
	for _, rel := range delta.Relationships {
		rels[rel.FromNodeID+"|"+rel.RelationshipType+"|"+rel.ToNodeID] = rel
	}
	want := []string{
		"unit:example.com/iface.Person|GO_SATISFIES|unit:example.com/iface.Greeter",
		"unit:example.com/iface.LoudPerson|GO_SATISFIES|unit:example.com/iface.Greeter",
		"unit:example.com/iface.LoudPerson|GO_SATISFIES|unit:example.com/iface.Shouter",
		"unit:example.com/iface.LoudPerson|GO_EMBEDS|unit:example.com/iface.Person",
		"unit:example.com/iface.Shouter|GO_EMBEDS|unit:example.com/iface.Greeter",
		"fn:example.com/iface.Person.Greet|GO_METHOD_SATISFIES|fn:example.com/iface.Greeter.Greet",
		"fn:example.com/iface.LoudPerson.Shout|GO_METHOD_SATISFIES|fn:example.com/iface.Shouter.Shout",
		"fn:example.com/iface.EmbeddedChild.Ping|GO_METHOD_SATISFIES|fn:example.com/iface.EmbeddedBase.Ping",
		"unit:example.com/iface.CrossPackageGreeter|GO_SATISFIES|unit:example.com/iface/contract.ExternalGreeter",
		"fn:example.com/iface.CrossPackageGreeter.ExternalGreet|GO_METHOD_SATISFIES|fn:example.com/iface/contract.ExternalGreeter.ExternalGreet",
		"unit:example.com/iface.PointerPerson|GO_SATISFIES|unit:example.com/iface.PointerGreeter",
		"fn:example.com/iface.PointerPerson.PointerGreet|GO_METHOD_SATISFIES|fn:example.com/iface.PointerGreeter.PointerGreet",
	}
	for _, key := range want {
		rel, ok := rels[key]
		if !ok {
			t.Errorf("missing exact relationship %s", key)
			continue
		}
		parts := strings.Split(key, "|")
		if expectedID := ids.RelationshipID(parts[0], parts[1], parts[2]); rel.ID != expectedID {
			t.Errorf("relationship %s has id %s, want %s", key, rel.ID, expectedID)
		}
		expectedKind, expectedFrom, expectedTo := protocol.RelationshipContract(parts[1])
		if rel.RelationshipKind != expectedKind || rel.FromNodeType != expectedFrom || rel.ToNodeType != expectedTo {
			t.Errorf("relationship %s has contract %s/%s/%s, want %s/%s/%s",
				key, rel.RelationshipKind, rel.FromNodeType, rel.ToNodeType,
				expectedKind, expectedFrom, expectedTo)
		}
	}
	for _, forbidden := range []string{
		"unit:example.com/iface.Almost|GO_SATISFIES|unit:example.com/iface.Partial",
		"fn:example.com/iface.Almost.First|GO_METHOD_SATISFIES|fn:example.com/iface.Partial.First",
		"fn:example.com/iface.LoudPerson.Greet|GO_METHOD_SATISFIES|fn:example.com/iface.Greeter.Greet",
	} {
		if _, ok := rels[forbidden]; ok {
			t.Errorf("unexpected relationship %s", forbidden)
		}
	}
	ids := map[string]bool{}
	for _, node := range delta.Packages {
		ids[node.ID] = true
	}
	for _, node := range delta.Units {
		ids[node.ID] = true
	}
	for _, node := range delta.Functions {
		ids[node.ID] = true
	}
	for _, rel := range delta.Relationships {
		if !ids[rel.FromNodeID] || !ids[rel.ToNodeID] {
			t.Errorf("dangling relationship: %+v", rel)
		}
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
	rule := strings.Replace(string(b), "path: path", "path: path\n  other: \"configured-by-ser\"", 1)
	delta, err := parse.Parse(protocol.ParseRequest{
		ProjectName: "http",
		Language:    "go",
		ProjectRoot: ex,
		RuleSources: []string{rule},
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
	for _, endpoint := range delta.Endpoints {
		if endpoint["other"] != "configured-by-ser" {
			t.Fatalf("endpoint other=%#v", endpoint["other"])
		}
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
	if len(delta.Endpoints) != 0 || len(delta.Diagnostics) != 0 {
		t.Fatalf("no SER should keep the base graph without endpoint errors: endpoints=%d diagnostics=%v", len(delta.Endpoints), delta.Diagnostics)
	}
}

func TestMethodDictionaryMaterializesFourEndpointTypes(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "../../testdata/module")
	root, _ = filepath.Abs(root)
	type endpointCase struct {
		typeName  string
		direction string
		field     string
		value     string
		identity  string
	}
	cases := []endpointCase{
		{"HTTP", "inbound", "path", "/users/{userId}", "HTTP:GET:/users/{userId}"},
		{"MQ", "outbound", "topic", "users.changed", "MQ:users.changed"},
		{"REDIS", "inbound", "keyPattern", "user:*", "REDIS:user:*"},
		{"DB", "outbound", "tableName", "users", "DB:users"},
	}
	var rules []string
	for _, item := range cases {
		extra := ""
		if item.typeName == "REDIS" {
			extra = "  command: \"DEL\"\n"
		}
		rules = append(rules, fmt.Sprintf(`
rule "Configured %s"
endpoint %s %s
find method User.Greet
let identity =
  from method take value
let handler =
  from method take name
build {
  endpointType: "%s"
  direction: "%s"
  method: "GET"
  %s: identity
%s
  handler: handler
  other: "metadata"
}
dict {
  example.com/demo.User.Greet() = %s
}
`, item.typeName, item.typeName, item.direction, item.typeName, item.direction, item.field, extra, item.value))
	}
	delta, err := parse.Parse(protocol.ParseRequest{
		ProjectName: "demo",
		Language:    "go",
		ProjectRoot: root,
		RuleSources: rules,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(delta.Endpoints) != len(cases) {
		t.Fatalf("endpoints=%#v diagnostics=%v", delta.Endpoints, delta.Diagnostics)
	}
	byIdentity := map[string]map[string]any{}
	for _, endpoint := range delta.Endpoints {
		byIdentity[endpoint["matchIdentity"].(string)] = endpoint
	}
	for _, item := range cases {
		endpoint := byIdentity[item.identity]
		if endpoint == nil {
			t.Errorf("missing %s in %#v", item.identity, byIdentity)
			continue
		}
		if endpoint["parseLevel"] != "config" || endpoint["other"] != "metadata" || endpoint["isExternal"] != (item.direction == "outbound") {
			t.Errorf("endpoint %s=%#v", item.identity, endpoint)
		}
		if endpoint[item.field] != item.value {
			t.Errorf("endpoint %s field %s=%#v", item.identity, item.field, endpoint[item.field])
		}
		if item.typeName == "REDIS" && endpoint["command"] != "DELETE" {
			t.Errorf("Redis DEL was not canonicalized: %#v", endpoint)
		}
		if item.typeName == "HTTP" && endpoint["normalizedPath"] != item.value {
			t.Errorf("parser changed exact HTTP path: endpoint=%#v", endpoint)
		}
		if item.typeName != "HTTP" && (endpoint["httpMethod"] != nil || endpoint["path"] != nil || endpoint["normalizedPath"] != nil) {
			t.Errorf("non-HTTP endpoint leaked HTTP fields: %#v", endpoint)
		}
	}
	links := relTypes(delta)
	if links[protocol.RelEndpointToFunc] != 2 || links[protocol.RelFunctionToEndpoint] != 2 {
		t.Fatalf("endpoint links=%v", links)
	}
}

// local helper to avoid importing os in every test file style
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
