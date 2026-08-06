package parse

import (
	"path/filepath"
	"strings"

	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"golang.org/x/tools/go/packages"
)

// Context is shared state while building one GraphDelta.
type Context struct {
	Req     protocol.ParseRequest
	Root    string // absolute project root
	Pkgs    []*packages.Package
	Delta   *protocol.GraphDelta
	// index for structure / call edges
	UnitByQName map[string]string // qualifiedName -> unit id
	FuncByQName map[string]string // qualifiedName/signature -> fn id
	SeenRel     map[string]bool
}

func newContext(req protocol.ParseRequest, root string, pkgs []*packages.Package) *Context {
	d := protocol.EmptyDelta(req)
	d.Scope.ProjectRoot = root
	d.Scope.Language = "go"
	if d.Scope.ProjectName == "" {
		d.Scope.ProjectName = filepath.Base(root)
	}
	return &Context{
		Req:         req,
		Root:        root,
		Pkgs:        pkgs,
		Delta:       &d,
		UnitByQName: map[string]string{},
		FuncByQName: map[string]string{},
		SeenRel:     map[string]bool{},
	}
}

func (c *Context) projectName() string { return c.Delta.Scope.ProjectName }

func (c *Context) relPath(abs string) string {
	if abs == "" {
		return "go.mod"
	}
	if !filepath.IsAbs(abs) {
		if a, err := filepath.Abs(abs); err == nil {
			abs = a
		}
	}
	if r, err := filepath.Rel(c.Root, abs); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(abs)
}

func (c *Context) addRel(rel protocol.CodeRelationship) {
	if rel.FromNodeID == "" || rel.ToNodeID == "" || rel.RelationshipType == "" {
		return
	}
	key := rel.FromNodeID + "|" + rel.RelationshipType + "|" + rel.ToNodeID
	if c.SeenRel[key] {
		return
	}
	c.SeenRel[key] = true
	if rel.ID == "" {
		// filled by caller via ids package
	}
	if rel.Language == "" {
		rel.Language = "go"
	}
	if rel.ProjectName == "" {
		rel.ProjectName = c.projectName()
	}
	c.Delta.Relationships = append(c.Delta.Relationships, rel)
}

func (c *Context) diag(msg string) {
	c.Delta.Diagnostics = append(c.Delta.Diagnostics, protocol.Diagnostic{
		Level:    "ERROR",
		Severity: "ERROR",
		Message:  msg,
	})
}

func baseIdent(s string) string {
	s = strings.TrimPrefix(s, "*")
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

func intPtr(v int) *int { return &v }
