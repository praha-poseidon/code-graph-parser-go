package parse

import (
	"go/ast"
	"strings"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"golang.org/x/tools/go/packages"
)

// collectPackagesUnitsFunctions fills packages, units, functions and structure relationships.
func collectPackagesUnitsFunctions(c *Context) {
	seenPkg := map[string]bool{}

	for _, pkg := range c.Pkgs {
		if pkg.PkgPath == "" || strings.HasSuffix(pkg.PkgPath, ".test") {
			continue
		}
		pkgID := ids.PackageID(pkg.PkgPath)
		filePath := packageFilePath(c, pkg)

		if !seenPkg[pkgID] {
			seenPkg[pkgID] = true
			c.Delta.Packages = append(c.Delta.Packages, protocol.CodePackage{
				ID:              pkgID,
				Name:            packageName(pkg),
				QualifiedName:   pkg.PkgPath,
				Language:        "go",
				ProjectName:     c.projectName(),
				ProjectFilePath: filePath,
				GitRepoURL:      c.Req.GitRepoURL,
				GitBranch:       c.Req.GitBranch,
				PackagePath:     strings.ReplaceAll(pkg.PkgPath, ".", "/"),
			})
		}

		for _, file := range pkg.Syntax {
			rel := c.relPath(pkg.Fset.Position(file.Pos()).Filename)
			ast.Inspect(file, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.TypeSpec:
					if x.Name == nil {
						return true
					}
					emitUnit(c, pkg, pkgID, rel, x)
				case *ast.FuncDecl:
					if x.Name == nil {
						return true
					}
					emitFunction(c, pkg, pkgID, rel, x)
				}
				return true
			})
		}
	}
}

func packageName(pkg *packages.Package) string {
	if pkg.Name != "" {
		return pkg.Name
	}
	if i := strings.LastIndex(pkg.PkgPath, "/"); i >= 0 {
		return pkg.PkgPath[i+1:]
	}
	return pkg.PkgPath
}

func packageFilePath(c *Context, pkg *packages.Package) string {
	if len(pkg.GoFiles) > 0 {
		return c.relPath(pkg.GoFiles[0])
	}
	if len(pkg.CompiledGoFiles) > 0 {
		return c.relPath(pkg.CompiledGoFiles[0])
	}
	// fallback: go.mod at root
	return "go.mod"
}

func emitUnit(c *Context, pkg *packages.Package, pkgID, rel string, ts *ast.TypeSpec) {
	name := ts.Name.Name
	qname := pkg.PkgPath + "." + name
	unitType := "class" // default: struct / alias → class-like for engine
	switch ts.Type.(type) {
	case *ast.InterfaceType:
		unitType = "interface"
	case *ast.StructType:
		unitType = "class"
	}

	pos := pkg.Fset.Position(ts.Pos())
	end := pkg.Fset.Position(ts.End())
	id := ids.UnitID(qname)
	c.UnitByQName[qname] = id
	c.UnitByQName[name] = id // local short name within package (last write wins)

	c.Delta.Units = append(c.Delta.Units, protocol.CodeUnit{
		ID:              id,
		Name:            name,
		QualifiedName:   qname,
		Language:        "go",
		ProjectName:     c.projectName(),
		ProjectFilePath: rel,
		GitRepoURL:      c.Req.GitRepoURL,
		GitBranch:       c.Req.GitBranch,
		UnitType:        unitType,
		PackageID:       pkgID,
		StartLine:       intPtr(pos.Line),
		EndLine:         intPtr(end.Line),
	})

	c.addRel(protocol.CodeRelationship{
		ID:               ids.RelationshipID(pkgID, protocol.RelPackageToUnit, id),
		FromNodeID:       pkgID,
		ToNodeID:         id,
		RelationshipType: protocol.RelPackageToUnit,
		Language:         "go",
		ProjectName:      c.projectName(),
	})
}

func emitFunction(c *Context, pkg *packages.Package, pkgID, rel string, fd *ast.FuncDecl) {
	name := fd.Name.Name
	recvType := ""
	if fd.Recv != nil && len(fd.Recv.List) > 0 {
		recvType = typeExprString(fd.Recv.List[0].Type)
	}
	sig := functionSignature(name, recvType, fd)
	qname := pkg.PkgPath + "." + sig
	id := ids.FunctionID(qname)
	c.FuncByQName[qname] = id
	c.FuncByQName[pkg.PkgPath+"."+name] = id

	pos := pkg.Fset.Position(fd.Pos())
	end := pkg.Fset.Position(fd.End())
	ret := ""
	if fd.Type != nil && fd.Type.Results != nil {
		var parts []string
		for _, f := range fd.Type.Results.List {
			parts = append(parts, typeExprString(f.Type))
		}
		ret = strings.Join(parts, ",")
	}

	c.Delta.Functions = append(c.Delta.Functions, protocol.CodeFunction{
		ID:              id,
		Name:            name,
		QualifiedName:   qname,
		Language:        "go",
		ProjectName:     c.projectName(),
		ProjectFilePath: rel,
		GitRepoURL:      c.Req.GitRepoURL,
		GitBranch:       c.Req.GitBranch,
		Signature:       sig,
		ReturnType:      ret,
		StartLine:       intPtr(pos.Line),
		EndLine:         intPtr(end.Line),
	})

	// UNIT_TO_FUNCTION for methods; free funcs attach to package via DECLARES-like PACKAGE? Engine only has UNIT_TO_FUNCTION.
	// Free functions: create synthetic unit per file? Java always has class. For Go package-level funcs,
	// attach UNIT_TO_FUNCTION only when receiver type unit exists; otherwise skip structure edge
	// (function still listed; package CONTAIN via no PACKAGE_TO_FUNCTION in enum).
	if recvType != "" {
		unitQ := pkg.PkgPath + "." + baseIdent(recvType)
		if unitID, ok := c.UnitByQName[unitQ]; ok {
			c.addRel(protocol.CodeRelationship{
				ID:               ids.RelationshipID(unitID, protocol.RelUnitToFunction, id),
				FromNodeID:       unitID,
				ToNodeID:         id,
				RelationshipType: protocol.RelUnitToFunction,
				Language:         "go",
				ProjectName:      c.projectName(),
			})
		}
	} else {
		// Package-level function: use a synthetic file unit so structure graph stays connected.
		// Prefer linking via a synthetic unit id unit:<pkg>.(package) only if we emit it.
		// Simpler approach matching many Go graphs: emit unit for package as "package functions container"
		// Actually validator allows UNIT_TO_FUNCTION only Unit->Function. Emit synthetic unit per package for free funcs.
		synthQ := pkg.PkgPath + ".(package)"
		synthID := ids.UnitID(synthQ)
		if _, ok := c.UnitByQName[synthQ]; !ok {
			c.UnitByQName[synthQ] = synthID
			c.Delta.Units = append(c.Delta.Units, protocol.CodeUnit{
				ID:              synthID,
				Name:            "(package)",
				QualifiedName:   synthQ,
				Language:        "go",
				ProjectName:     c.projectName(),
				ProjectFilePath: rel,
				UnitType:        "class",
				PackageID:       pkgID,
			})
			c.addRel(protocol.CodeRelationship{
				ID:               ids.RelationshipID(pkgID, protocol.RelPackageToUnit, synthID),
				FromNodeID:       pkgID,
				ToNodeID:         synthID,
				RelationshipType: protocol.RelPackageToUnit,
				Language:         "go",
				ProjectName:      c.projectName(),
			})
		}
		c.addRel(protocol.CodeRelationship{
			ID:               ids.RelationshipID(synthID, protocol.RelUnitToFunction, id),
			FromNodeID:       synthID,
			ToNodeID:         id,
			RelationshipType: protocol.RelUnitToFunction,
			Language:         "go",
			ProjectName:      c.projectName(),
		})
	}
}

func functionSignature(name, recv string, fd *ast.FuncDecl) string {
	if recv != "" {
		return baseIdent(recv) + "." + name
	}
	return name
}

func typeExprString(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeExprString(t.X)
	case *ast.SelectorExpr:
		return typeExprString(t.X) + "." + t.Sel.Name
	case *ast.IndexExpr:
		return typeExprString(t.X)
	case *ast.IndexListExpr:
		return typeExprString(t.X)
	case *ast.ArrayType:
		return "[]" + typeExprString(t.Elt)
	case *ast.MapType:
		return "map"
	case *ast.InterfaceType:
		return "interface"
	case *ast.StructType:
		return "struct"
	case *ast.FuncType:
		return "func"
	case *ast.ChanType:
		return "chan"
	case *ast.Ellipsis:
		return "..." + typeExprString(t.Elt)
	default:
		return ""
	}
}
