package parse

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"golang.org/x/tools/go/packages"
)

// collectPackagesUnitsFunctions fills packages, units, functions and structure relationships.
func collectPackagesUnitsFunctions(c *Context) {
	seenPkg := map[string]bool{}

	for _, pkg := range c.Pkgs {
		if pkg.PkgPath == "" || isTestPackage(pkg) {
			continue
		}
		pkgID := ids.PackageID(pkg.PkgPath)
		filePath := packageFilePath(c, pkg)

		// Skip package entirely if incremental filter excludes all its files
		if c.FileAllow != nil && !packageHasAllowedFile(c, pkg) {
			continue
		}

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

		// Ensure package-level synthetic unit exists when free funcs appear
		for _, file := range pkg.Syntax {
			abs := pkg.Fset.Position(file.Pos()).Filename
			rel := c.relPath(abs)
			if !c.allowFile(rel) {
				continue
			}
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

		// Interface methods as function nodes (for OVERRIDES / IMPLEMENTS method links)
		emitInterfaceMethodsFromTypes(c, pkg, pkgID)
	}
}

func isTestPackage(pkg *packages.Package) bool {
	return strings.HasSuffix(pkg.PkgPath, ".test") || strings.HasSuffix(pkg.Name, "_test")
}

func packageHasAllowedFile(c *Context, pkg *packages.Package) bool {
	for _, f := range pkg.GoFiles {
		if c.allowFile(c.relPath(f)) {
			return true
		}
	}
	for _, f := range pkg.CompiledGoFiles {
		if c.allowFile(c.relPath(f)) {
			return true
		}
	}
	return false
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
	for _, f := range pkg.GoFiles {
		rel := c.relPath(f)
		if c.allowFile(rel) {
			return rel
		}
	}
	if len(pkg.GoFiles) > 0 {
		return c.relPath(pkg.GoFiles[0])
	}
	if len(pkg.CompiledGoFiles) > 0 {
		return c.relPath(pkg.CompiledGoFiles[0])
	}
	return "go.mod"
}

func emitUnit(c *Context, pkg *packages.Package, pkgID, rel string, ts *ast.TypeSpec) {
	name := ts.Name.Name
	qname := pkg.PkgPath + "." + name
	unitType := "class"
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

	var mods []string
	if ts.Assign.IsValid() {
		// type alias
		mods = append(mods, "alias")
	}

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
		Modifiers:       mods,
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
	sig := functionSignature(name, recvType)
	qname := pkg.PkgPath + "." + sig
	id := ids.FunctionID(qname)
	c.FuncByQName[qname] = id
	c.FuncByQName[pkg.PkgPath+"."+name] = id

	pos := pkg.Fset.Position(fd.Pos())
	end := pkg.Fset.Position(fd.End())
	// index body start for endpoint handler resolution
	c.FuncAtFileLine[rel+":"+itoa(pos.Line)] = id
	if fd.Body != nil {
		bodyLine := pkg.Fset.Position(fd.Body.Pos()).Line
		c.FuncAtFileLine[rel+":"+itoa(bodyLine)] = id
	}
	// register all lines in function range for endpoint matching
	for line := pos.Line; line <= end.Line; line++ {
		c.FuncAtFileLine[rel+":"+itoa(line)] = id
	}

	ret := resultTypes(fd)
	var mods []string
	if name == "init" {
		mods = append(mods, "init")
	}
	isCtor := name == "New" || strings.HasPrefix(name, "New")

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
		Modifiers:       mods,
		IsConstructor:   boolPtr(isCtor),
		StartLine:       intPtr(pos.Line),
		EndLine:         intPtr(end.Line),
	})

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
		synthQ := pkg.PkgPath + ".(package)"
		synthID := ensurePackageUnit(c, pkg, pkgID, rel, synthQ)
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

func ensurePackageUnit(c *Context, pkg *packages.Package, pkgID, rel, synthQ string) string {
	if id, ok := c.UnitByQName[synthQ]; ok {
		return id
	}
	synthID := ids.UnitID(synthQ)
	c.UnitByQName[synthQ] = synthID
	c.Delta.Units = append(c.Delta.Units, protocol.CodeUnit{
		ID:              synthID,
		Name:            "(package)",
		QualifiedName:   synthQ,
		Language:        "go",
		ProjectName:     c.projectName(),
		ProjectFilePath: rel,
		GitRepoURL:      c.Req.GitRepoURL,
		GitBranch:       c.Req.GitBranch,
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
	return synthID
}

// emitInterfaceMethodsFromTypes adds CodeFunction nodes for interface methods so OVERRIDES can target them.
func emitInterfaceMethodsFromTypes(c *Context, pkg *packages.Package, pkgID string) {
	if pkg.Types == nil {
		return
	}
	scope := pkg.Types.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		tn, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}
		iface, ok := tn.Type().Underlying().(*types.Interface)
		if !ok {
			continue
		}
		unitQ := pkg.PkgPath + "." + tn.Name()
		unitID, ok := c.UnitByQName[unitQ]
		if !ok {
			continue
		}
		// find file path from unit
		rel := "go.mod"
		for _, u := range c.Delta.Units {
			if u.ID == unitID {
				rel = u.ProjectFilePath
				break
			}
		}
		if !c.allowFile(rel) && rel != "go.mod" {
			continue
		}
		for i := 0; i < iface.NumExplicitMethods(); i++ {
			m := iface.ExplicitMethod(i)
			sig := tn.Name() + "." + m.Name()
			qname := pkg.PkgPath + "." + sig
			id := ids.FunctionID(qname)
			if _, exists := c.FuncByQName[qname]; exists {
				continue
			}
			c.FuncByQName[qname] = id
			ret := ""
			if s, ok := m.Type().(*types.Signature); ok && s.Results() != nil {
				var parts []string
				for j := 0; j < s.Results().Len(); j++ {
					parts = append(parts, types.TypeString(s.Results().At(j).Type(), (*types.Package).Name))
				}
				ret = strings.Join(parts, ",")
			}
			c.Delta.Functions = append(c.Delta.Functions, protocol.CodeFunction{
				ID:              id,
				Name:            m.Name(),
				QualifiedName:   qname,
				Language:        "go",
				ProjectName:     c.projectName(),
				ProjectFilePath: rel,
				Signature:       sig,
				ReturnType:      ret,
				Modifiers:       []string{"interface"},
			})
			c.addRel(protocol.CodeRelationship{
				ID:               ids.RelationshipID(unitID, protocol.RelUnitToFunction, id),
				FromNodeID:       unitID,
				ToNodeID:         id,
				RelationshipType: protocol.RelUnitToFunction,
				Language:         "go",
				ProjectName:      c.projectName(),
			})
		}
	}
}

func functionSignature(name, recv string) string {
	if recv != "" {
		return baseIdent(recv) + "." + name
	}
	return name
}

func resultTypes(fd *ast.FuncDecl) string {
	if fd.Type == nil || fd.Type.Results == nil {
		return ""
	}
	var parts []string
	for _, f := range fd.Type.Results.List {
		parts = append(parts, typeExprString(f.Type))
	}
	return strings.Join(parts, ",")
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
	case *ast.ParenExpr:
		return typeExprString(t.X)
	default:
		return ""
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// ensure token import used for Assign check
var _ = token.NoPos
