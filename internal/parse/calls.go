package parse

import (
	"go/ast"
	"go/types"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"golang.org/x/tools/go/packages"
)

func collectCalls(c *Context) {
	for _, pkg := range c.Pkgs {
		if pkg.TypesInfo == nil {
			continue
		}
		for _, file := range pkg.Syntax {
			var currentFn string
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.FuncDecl:
					if x.Name == nil {
						return true
					}
					recv := ""
					if x.Recv != nil && len(x.Recv.List) > 0 {
						recv = typeExprString(x.Recv.List[0].Type)
					}
					sig := functionSignature(x.Name.Name, recv, x)
					currentFn = ids.FunctionID(pkg.PkgPath + "." + sig)
				case *ast.CallExpr:
					if currentFn == "" {
						return true
					}
					toIDs, callType := resolveCallee(pkg, x)
					line := pkg.Fset.Position(x.Pos()).Line
					for _, to := range toIDs {
						if to == "" || to == currentFn {
							continue
						}
						c.addRel(protocol.CodeRelationship{
							ID:               ids.RelationshipID(currentFn, protocol.RelCalls, to),
							FromNodeID:       currentFn,
							ToNodeID:         to,
							RelationshipType: protocol.RelCalls,
							LineNumber:       intPtr(line),
							CallType:         callType,
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

func resolveCallee(pkg *packages.Package, ce *ast.CallExpr) (idsOut []string, callType string) {
	info := pkg.TypesInfo
	callType = "direct"
	if info == nil {
		return fallbackCallee(pkg, ce), callType
	}

	switch f := ce.Fun.(type) {
	case *ast.Ident:
		if obj := info.Uses[f]; obj != nil {
			if fn, ok := obj.(*types.Func); ok {
				return []string{typesFuncID(fn)}, callType
			}
		}
	case *ast.SelectorExpr:
		if sel := info.Selections[f]; sel != nil {
			callType = "method"
			if fn, ok := sel.Obj().(*types.Func); ok {
				return []string{typesFuncID(fn)}, callType
			}
		}
		if obj := info.Uses[f.Sel]; obj != nil {
			if fn, ok := obj.(*types.Func); ok {
				return []string{typesFuncID(fn)}, "static"
			}
		}
	case *ast.IndexExpr, *ast.IndexListExpr:
		// generic instantiation: unwrap
		if ix, ok := ce.Fun.(*ast.IndexExpr); ok {
			return resolveCallee(pkg, &ast.CallExpr{Fun: ix.X, Args: ce.Args})
		}
	}
	return fallbackCallee(pkg, ce), callType
}

func typesFuncID(fn *types.Func) string {
	pkgPath := ""
	if fn.Pkg() != nil {
		pkgPath = fn.Pkg().Path()
	}
	sig := fn.Name()
	if t, ok := fn.Type().(*types.Signature); ok && t.Recv() != nil {
		recv := types.TypeString(t.Recv().Type(), func(p *types.Package) string {
			if p == nil {
				return ""
			}
			return p.Name()
		})
		sig = baseIdent(recv) + "." + fn.Name()
	}
	return ids.FunctionID(pkgPath + "." + sig)
}

func fallbackCallee(pkg *packages.Package, ce *ast.CallExpr) []string {
	switch f := ce.Fun.(type) {
	case *ast.Ident:
		return []string{ids.FunctionID(pkg.PkgPath + "." + f.Name)}
	case *ast.SelectorExpr:
		// pkg.Func or x.Method
		if id, ok := f.X.(*ast.Ident); ok {
			// could be package name or variable
			return []string{ids.FunctionID(id.Name + "." + f.Sel.Name)}
		}
		return []string{ids.FunctionID(pkg.PkgPath + "." + f.Sel.Name)}
	default:
		return nil
	}
}
