package parse

import (
	"path/filepath"
	"strings"

	"github.com/praha-poseidon/code-graph-parser-go/internal/gopls"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"golang.org/x/tools/go/packages"
)

// Context is shared state while building one GraphDelta.
type Context struct {
	Req   protocol.ParseRequest
	Root  string
	Pkgs  []*packages.Package
	Delta *protocol.GraphDelta

	UnitByQName map[string]string // pkg.Type -> unit id
	FuncByQName map[string]string // qualified signature -> fn id
	// FuncAtFileLine: "rel/path.go:line" -> fn id (for endpoint linking)
	FuncAtFileLine map[string]string
	// FileAllow: if non-nil, only emit nodes from these project-relative paths
	FileAllow map[string]bool
	SeenRel   map[string]bool
	// Placeholder functions emitted for external call targets
	PlaceholderFns map[string]bool
	// Gopls is present when the CLI runs in task-scoped streaming mode.
	Gopls    *gopls.Session
	GoplsErr error
}

func newContext(req protocol.ParseRequest, root string, pkgs []*packages.Package) *Context {
	d := protocol.EmptyDelta(req)
	d.Scope.ProjectRoot = root
	d.Scope.Language = "go"
	if d.Scope.ProjectName == "" {
		d.Scope.ProjectName = filepath.Base(root)
	}
	c := &Context{
		Req:            req,
		Root:           root,
		Pkgs:           pkgs,
		Delta:          &d,
		UnitByQName:    map[string]string{},
		FuncByQName:    map[string]string{},
		FuncAtFileLine: map[string]string{},
		SeenRel:        map[string]bool{},
		PlaceholderFns: map[string]bool{},
	}
	if len(req.SourceFiles) > 0 {
		c.FileAllow = map[string]bool{}
		for _, f := range req.SourceFiles {
			c.FileAllow[c.normalizeAllow(f)] = true
		}
	}
	return c
}

func (c *Context) projectName() string { return c.Delta.Scope.ProjectName }

func (c *Context) normalizeAllow(p string) string {
	if !filepath.IsAbs(p) {
		p = filepath.Join(c.Root, p)
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return filepath.ToSlash(c.relPath(p))
}

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

func (c *Context) allowFile(rel string) bool {
	if c.FileAllow == nil {
		return true
	}
	rel = filepath.ToSlash(rel)
	if c.FileAllow[rel] {
		return true
	}
	// also allow if any allow entry is suffix match
	for a := range c.FileAllow {
		if strings.HasSuffix(rel, a) || strings.HasSuffix(a, rel) {
			return true
		}
	}
	return false
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
	if rel.Language == "" {
		rel.Language = "go"
	}
	if rel.ProjectName == "" {
		rel.ProjectName = c.projectName()
	}
	if rel.RelationshipKind == "" || rel.FromNodeType == "" || rel.ToNodeType == "" {
		rel.RelationshipKind, rel.FromNodeType, rel.ToNodeType = protocol.RelationshipContract(rel.RelationshipType)
	}
	c.Delta.Relationships = append(c.Delta.Relationships, rel)
}

func (c *Context) diag(level, msg string) {
	c.Delta.Diagnostics = append(c.Delta.Diagnostics, protocol.Diagnostic{
		Level:   level,
		Code:    "parser.go",
		Message: msg,
		Details: map[string]any{},
	})
}

func baseIdent(s string) string {
	s = strings.TrimPrefix(strings.TrimSpace(s), "*")
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}

func intPtr(v int) *int { return &v }

func boolPtr(v bool) *bool { return &v }
