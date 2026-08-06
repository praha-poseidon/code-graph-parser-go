package graph

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"golang.org/x/tools/go/packages"
)

func Build(req protocol.ParseRequest, pkgs []*packages.Package) protocol.GraphDelta {
	delta := protocol.GraphDelta{
		Scope: protocol.DeltaScope{
			ProjectName: req.ProjectName,
			Language:    "go",
			ProjectRoot: req.ProjectRoot,
			SourceFiles: req.SourceFiles,
		},
		Packages:               []protocol.CodePackage{},
		Units:                  []protocol.CodeUnit{},
		Functions:              []protocol.CodeFunction{},
		Endpoints:              []map[string]any{},
		Relationships:          []protocol.CodeRelationship{},
		DeletedNodeIds:         []string{},
		DeletedRelationshipIds: []string{},
		Diagnostics:            []protocol.Diagnostic{},
	}

	// pre-index function ids for same-package call resolution fallback
	fnIDs := map[string]bool{}

	seenPkg := map[string]bool{}
	for _, pkg := range pkgs {
		pkgID := "pkg:" + pkg.PkgPath
		if !seenPkg[pkgID] {
			seenPkg[pkgID] = true
			delta.Packages = append(delta.Packages, protocol.CodePackage{
				ID: pkgID, Name: pkg.PkgPath, Language: "go",
				ProjectFilePath: "", ProjectName: req.ProjectName,
			})
		}
		for _, file := range pkg.Syntax {
			pos := pkg.Fset.Position(file.Pos())
			rel := relPath(req.ProjectRoot, pos.Filename)

			// types + funcs first
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.TypeSpec:
					if x.Name == nil {
						return true
					}
					kind := "type"
					switch x.Type.(type) {
					case *ast.StructType:
						kind = "struct"
					case *ast.InterfaceType:
						kind = "interface"
					}
					id := fmt.Sprintf("unit:%s.%s", pkg.PkgPath, x.Name.Name)
					delta.Units = append(delta.Units, protocol.CodeUnit{
						ID: id, Name: x.Name.Name, Language: "go",
						ProjectFilePath: rel, ProjectName: req.ProjectName, UnitKind: kind,
					})
					delta.Relationships = append(delta.Relationships, relEdge(req, "CONTAINS", pkgID, id, rel))
				case *ast.FuncDecl:
					if x.Name == nil {
						return true
					}
					id, name, sig := funcID(pkg, x)
					fnIDs[id] = true
					p := pkg.Fset.Position(x.Pos())
					e := pkg.Fset.Position(x.End())
					delta.Functions = append(delta.Functions, protocol.CodeFunction{
						ID: id, Name: name, Language: "go",
						ProjectFilePath: rel, ProjectName: req.ProjectName,
						Signature: sig, StartLine: p.Line, EndLine: e.Line,
					})
					delta.Relationships = append(delta.Relationships, relEdge(req, "DECLARES", pkgID, id, rel))
					if x.Recv != nil {
						recv := exprName(x.Recv.List[0].Type)
						if recv != "" {
							unitID := fmt.Sprintf("unit:%s.%s", pkg.PkgPath, baseIdent(recv))
							delta.Relationships = append(delta.Relationships, relEdge(req, "DECLARES", unitID, id, rel))
						}
					}
				}
				return true
			})

			// imports
			for _, is := range file.Imports {
				path := strings.Trim(is.Path.Value, `"`)
				to := "pkg:" + path
				delta.Relationships = append(delta.Relationships, relEdge(req, "IMPORTS", pkgID, to, rel))
			}

			// CALLS with types.Info when available
			var currentFn string
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncDecl:
					if x.Name != nil {
						currentFn, _, _ = funcID(pkg, x)
					}
				case *ast.CallExpr:
					if currentFn == "" {
						return true
					}
					for _, to := range resolveCallees(pkg, x) {
						delta.Relationships = append(delta.Relationships, relEdge(req, "CALLS", currentFn, to, rel))
					}
				}
				return true
			})
		}
	}
	_ = fnIDs
	return delta
}

func funcID(pkg *packages.Package, fd *ast.FuncDecl) (id, name, sig string) {
	name = fd.Name.Name
	recv := ""
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recv = exprName(fd.Recv.List[0].Type)
	}
	sig = name
	if recv != "" {
		sig = baseIdent(recv) + "." + name
	}
	id = fmt.Sprintf("fn:%s.%s", pkg.PkgPath, sig)
	return id, name, sig
}

func resolveCallees(pkg *packages.Package, ce *ast.CallExpr) []string {
	info := pkg.TypesInfo
	if info != nil {
		switch f := ce.Fun.(type) {
		case *ast.Ident:
			if obj := info.Uses[f]; obj != nil {
				if fn, ok := obj.(*types.Func); ok {
					return []string{typesFuncID(fn)}
				}
			}
		case *ast.SelectorExpr:
			if obj := info.Uses[f.Sel]; obj != nil {
				if fn, ok := obj.(*types.Func); ok {
					return []string{typesFuncID(fn)}
				}
			}
			// method via selection
			if sel := info.Selections[f]; sel != nil {
				if fn, ok := sel.Obj().(*types.Func); ok {
					return []string{typesFuncID(fn)}
				}
			}
		}
	}
	// fallback name-based
	switch f := ce.Fun.(type) {
	case *ast.Ident:
		return []string{fmt.Sprintf("fn:%s.%s", pkg.PkgPath, f.Name)}
	case *ast.SelectorExpr:
		if id, ok := f.X.(*ast.Ident); ok {
			return []string{fmt.Sprintf("fn:%s.%s", id.Name, f.Sel.Name)}
		}
		return []string{fmt.Sprintf("fn:%s.%s", pkg.PkgPath, f.Sel.Name)}
	}
	return nil
}

func typesFuncID(fn *types.Func) string {
	pkgPath := ""
	if fn.Pkg() != nil {
		pkgPath = fn.Pkg().Path()
	}
	sig := fn.Name()
	if recv := fn.Type().(*types.Signature).Recv(); recv != nil {
		sig = baseIdent(types.TypeString(recv.Type(), func(p *types.Package) string {
			if p == nil {
				return ""
			}
			return p.Name()
		})) + "." + fn.Name()
	}
	return fmt.Sprintf("fn:%s.%s", pkgPath, sig)
}

func relEdge(req protocol.ParseRequest, typ, from, to, file string) protocol.CodeRelationship {
	raw := from + "|" + typ + "|" + to
	sum := sha1.Sum([]byte(raw))
	return protocol.CodeRelationship{
		ID: "rel:" + hex.EncodeToString(sum[:8]), Type: typ,
		FromNodeID: from, ToNodeID: to, Language: "go",
		ProjectFilePath: file, ProjectName: req.ProjectName,
	}
}

func relPath(root, abs string) string {
	if root == "" {
		return filepath.ToSlash(abs)
	}
	if !filepath.IsAbs(abs) {
		if a, err := filepath.Abs(abs); err == nil {
			abs = a
		}
	}
	if r, err := filepath.Rel(root, abs); err == nil {
		return filepath.ToSlash(r)
	}
	return filepath.ToSlash(abs)
}

func exprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return exprName(t.X)
	case *ast.SelectorExpr:
		return exprName(t.X) + "." + t.Sel.Name
	default:
		return ""
	}
}

func baseIdent(s string) string {
	s = strings.TrimPrefix(s, "*")
	if i := strings.LastIndex(s, "."); i >= 0 {
		return s[i+1:]
	}
	return s
}
