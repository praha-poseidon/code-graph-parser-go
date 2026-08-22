package parse

import (
	"go/ast"
	"go/types"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"golang.org/x/tools/go/packages"
)

// collectInheritance emits GO_EMBEDS for Go types.
// Go has no class extends; we map:
//   - struct embedding named types → GO_EMBEDS
//   - named interface embedding → GO_EMBEDS
//   - concrete type implementing interface (method set) is expensive; v1: interface embeds only + explicit embedding
func collectInheritance(c *Context) {
	for _, pkg := range c.Pkgs {
		if pkg.Types == nil || pkg.TypesInfo == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			tn, ok := obj.(*types.TypeName)
			if !ok {
				continue
			}
			fromQ := pkg.PkgPath + "." + tn.Name()
			fromID, ok := c.UnitByQName[fromQ]
			if !ok {
				continue
			}
			switch t := tn.Type().Underlying().(type) {
			case *types.Struct:
				for i := 0; i < t.NumFields(); i++ {
					f := t.Field(i)
					if !f.Embedded() {
						continue
					}
					toID := unitIDForType(c, pkg, f.Type())
					if toID == "" || toID == fromID {
						continue
					}
					c.addRel(protocol.CodeRelationship{
						ID:               ids.RelationshipID(fromID, protocol.RelEmbeds, toID),
						FromNodeID:       fromID,
						ToNodeID:         toID,
						RelationshipType: protocol.RelEmbeds,
						Language:         "go",
						ProjectName:      c.projectName(),
						CallType:         "embed",
					})
				}
			case *types.Interface:
				for i := 0; i < t.NumEmbeddeds(); i++ {
					toID := unitIDForType(c, pkg, t.EmbeddedType(i))
					if toID == "" || toID == fromID {
						continue
					}
					c.addRel(protocol.CodeRelationship{
						ID:               ids.RelationshipID(fromID, protocol.RelEmbeds, toID),
						FromNodeID:       fromID,
						ToNodeID:         toID,
						RelationshipType: protocol.RelEmbeds,
						Language:         "go",
						ProjectName:      c.projectName(),
						CallType:         "embed",
					})
				}
			}
		}

		// Also walk AST for type specs with embedding (redundant safety)
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				ts, ok := n.(*ast.TypeSpec)
				if !ok || ts.Name == nil {
					return true
				}
				fromQ := pkg.PkgPath + "." + ts.Name.Name
				fromID := c.UnitByQName[fromQ]
				if fromID == "" {
					return true
				}
				if st, ok := ts.Type.(*ast.StructType); ok && st.Fields != nil {
					for _, f := range st.Fields.List {
						if len(f.Names) != 0 {
							continue // not embedded
						}
						toID := unitIDForExpr(c, pkg, f.Type)
						if toID == "" || toID == fromID {
							continue
						}
						c.addRel(protocol.CodeRelationship{
							ID:               ids.RelationshipID(fromID, protocol.RelEmbeds, toID),
							FromNodeID:       fromID,
							ToNodeID:         toID,
							RelationshipType: protocol.RelEmbeds,
							Language:         "go",
							ProjectName:      c.projectName(),
						})
					}
				}
				if it, ok := ts.Type.(*ast.InterfaceType); ok && it.Methods != nil {
					for _, f := range it.Methods.List {
						if len(f.Names) != 0 {
							continue
						}
						toID := unitIDForExpr(c, pkg, f.Type)
						if toID == "" || toID == fromID {
							continue
						}
						c.addRel(protocol.CodeRelationship{
							ID:               ids.RelationshipID(fromID, protocol.RelEmbeds, toID),
							FromNodeID:       fromID,
							ToNodeID:         toID,
							RelationshipType: protocol.RelEmbeds,
							Language:         "go",
							ProjectName:      c.projectName(),
						})
					}
				}
				return true
			})
		}
	}
}

func unitIDForType(c *Context, pkg *packages.Package, t types.Type) string {
	t = types.Unalias(t)
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	if n, ok := t.(*types.Named); ok {
		obj := n.Obj()
		if obj == nil {
			return ""
		}
		path := pkg.PkgPath
		if obj.Pkg() != nil {
			path = obj.Pkg().Path()
		}
		q := path + "." + obj.Name()
		if id, ok := c.UnitByQName[q]; ok {
			return id
		}
		// external type: still emit unit id for stable edge (may not be in units list)
		return ids.UnitID(q)
	}
	return ""
}

func unitIDForExpr(c *Context, pkg *packages.Package, e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		q := pkg.PkgPath + "." + t.Name
		if id, ok := c.UnitByQName[q]; ok {
			return id
		}
		return ids.UnitID(q)
	case *ast.StarExpr:
		return unitIDForExpr(c, pkg, t.X)
	case *ast.SelectorExpr:
		// pkg.Type
		if id, ok := t.X.(*ast.Ident); ok {
			// resolve import path roughly by TypesInfo
			if pkg.TypesInfo != nil {
				if obj := pkg.TypesInfo.Uses[t.Sel]; obj != nil {
					if tn, ok := obj.(*types.TypeName); ok && tn.Pkg() != nil {
						q := tn.Pkg().Path() + "." + tn.Name()
						if uid, ok := c.UnitByQName[q]; ok {
							return uid
						}
						return ids.UnitID(q)
					}
				}
			}
			return ids.UnitID(id.Name + "." + t.Sel.Name)
		}
	}
	return ""
}
