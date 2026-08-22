package parse

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"github.com/praha-poseidon/code-graph-parser-go/internal/gopls"
	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"golang.org/x/mod/modfile"
)

type declarationKind int

const (
	declarationType declarationKind = iota + 1
	declarationMethod
)

type graphDeclaration struct {
	Kind        declarationKind
	Interface   bool
	ID          string
	Filename    string
	Line        int
	Column      int
	PackagePath string
	Owner       string
	Name        string
}

// collectGoplsImplementations augments an incremental delta with global Go
// implementation relations. The regular package load intentionally contains
// only the changed package(s); gopls searches its persistent per-package method
// set indexes for the rest of the workspace.
func collectGoplsImplementations(c *Context) {
	if c.FileAllow == nil || !optionBool(c.Req.Options, "goplsEnabled", true) {
		return
	}

	client := gopls.Client{
		Command:  optionString(c.Req.Options, "goplsCommand", os.Getenv("CODEGRAPH_GOPLS_COMMAND")),
		Root:     c.Root,
		CacheDir: optionString(c.Req.Options, "goplsCacheDir", filepath.Join(c.Root, ".codegraph-cache", "gopls")),
	}
	if c.GoplsErr != nil {
		handleGoplsUnavailable(c, c.GoplsErr)
		return
	}
	if c.Gopls != nil {
		c.Delta.Scope.Attributes["goplsImplementation"] = "enabled"
		collectGoplsImplementationResults(c, implementationQueries(c), func(positions []gopls.Location, concurrency int) ([]gopls.QueryResult, error) {
			return c.Gopls.Implementations(context.Background(), positions, concurrency)
		})
		return
	}
	if err := client.Available(); err != nil {
		handleGoplsUnavailable(c, err)
		return
	}
	c.Delta.Scope.Attributes["goplsImplementation"] = "enabled"

	collectGoplsImplementationResults(c, implementationQueries(c), func(positions []gopls.Location, concurrency int) ([]gopls.QueryResult, error) {
		return client.Implementations(context.Background(), positions, concurrency)
	})
}

func handleGoplsUnavailable(c *Context, err error) {
	if errors.Is(err, gopls.ErrUnavailable) {
		c.Delta.Scope.Attributes["goplsImplementation"] = "unavailable"
		if optionBool(c.Req.Options, "goplsRequired", false) {
			c.diag("ERROR", err.Error())
		}
		return
	}
	c.diag("WARN", err.Error())
}

type implementationBatch func([]gopls.Location, int) ([]gopls.QueryResult, error)

func collectGoplsImplementationResults(c *Context, queries []graphDeclaration, batch implementationBatch) {
	resolver := declarationResolver{root: c.Root, files: map[string][]graphDeclaration{}}
	queryErrors := map[string]bool{}
	results := runImplementationQueries(batch, queries, optionInt(c.Req.Options, "goplsConcurrency", 4))
	for _, resultSet := range results {
		query := resultSet.query
		if resultSet.err != nil {
			message := resultSet.err.Error()
			if !queryErrors[message] {
				queryErrors[message] = true
				c.diag("WARN", message)
			}
			continue
		}
		for _, location := range resultSet.locations {
			result, ok, err := resolver.at(location)
			if err != nil {
				message := "resolve gopls implementation: " + err.Error()
				if !queryErrors[message] {
					queryErrors[message] = true
					c.diag("WARN", message)
				}
				continue
			}
			if !ok || result.Kind != query.Kind || result.ID == query.ID {
				continue
			}
			emitGoplsImplementation(c, query, result)
		}
	}
}

type implementationQueryResult struct {
	query     graphDeclaration
	locations []gopls.Location
	err       error
}

func runImplementationQueries(batchFn implementationBatch, queries []graphDeclaration, concurrency int) []implementationQueryResult {
	results := make([]implementationQueryResult, len(queries))
	if len(queries) == 0 {
		return results
	}
	positions := make([]gopls.Location, len(queries))
	for index, query := range queries {
		positions[index] = gopls.Location{
			Filename: query.Filename,
			Line:     query.Line,
			Column:   query.Column,
		}
	}
	batch, err := batchFn(positions, concurrency)
	if err != nil {
		for index, query := range queries {
			results[index] = implementationQueryResult{query: query, err: err}
		}
		return results
	}
	for index, query := range queries {
		results[index] = implementationQueryResult{
			query: query, locations: batch[index].Locations, err: batch[index].Err,
		}
	}
	return results
}

func emitGoplsImplementation(c *Context, query, result graphDeclaration) {
	var implementation, contract graphDeclaration
	if query.Interface && !result.Interface {
		implementation, contract = result, query
	} else if !query.Interface && result.Interface {
		implementation, contract = query, result
	} else {
		// Interface-to-interface relationships remain EXTENDS and are emitted
		// from explicit embedding by collectInheritance.
		return
	}

	relType := protocol.RelImplements
	callType := ""
	if query.Kind == declarationMethod {
		relType = protocol.RelOverrides
		callType = "interface"
	}
	c.addRel(protocol.CodeRelationship{
		ID:               ids.RelationshipID(implementation.ID, relType, contract.ID),
		FromNodeID:       implementation.ID,
		ToNodeID:         contract.ID,
		RelationshipType: relType,
		CallType:         callType,
		Language:         "go",
		ProjectName:      c.projectName(),
	})
}

func implementationQueries(c *Context) []graphDeclaration {
	seen := map[string]bool{}
	var queries []graphDeclaration
	add := func(query graphDeclaration) {
		key := query.Filename + ":" + itoa(query.Line) + ":" + itoa(query.Column)
		if seen[key] {
			return
		}
		seen[key] = true
		queries = append(queries, query)
	}

	for _, pkg := range c.Pkgs {
		if pkg.Types == nil || pkg.Fset == nil {
			continue
		}
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			typeName, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || typeName.IsAlias() {
				continue
			}
			named, ok := typeName.Type().(*types.Named)
			if !ok {
				continue
			}
			position := pkg.Fset.Position(typeName.Pos())
			if position.Filename == "" || !c.allowFile(c.relPath(position.Filename)) {
				continue
			}
			isInterface := types.IsInterface(named)
			if types.NewMethodSet(named).Len() > 0 || types.NewMethodSet(types.NewPointer(named)).Len() > 0 {
				add(graphDeclaration{
					Kind: declarationType, Interface: isInterface,
					ID:       ids.UnitID(pkg.PkgPath + "." + typeName.Name()),
					Filename: position.Filename, Line: position.Line, Column: position.Column,
					PackagePath: pkg.PkgPath, Owner: typeName.Name(), Name: typeName.Name(),
				})
			}

			if isInterface {
				iface := named.Underlying().(*types.Interface)
				for i := 0; i < iface.NumExplicitMethods(); i++ {
					method := iface.ExplicitMethod(i)
					addMethodQuery(c, pkg.PkgPath, typeName.Name(), method, true, pkg.Fset, add)
				}
				continue
			}
			for i := 0; i < named.NumMethods(); i++ {
				addMethodQuery(c, pkg.PkgPath, typeName.Name(), named.Method(i), false, pkg.Fset, add)
			}
		}
	}
	return queries
}

func addMethodQuery(c *Context, pkgPath, owner string, method *types.Func, isInterface bool, fset *token.FileSet, add func(graphDeclaration)) {
	position := fset.Position(method.Pos())
	if position.Filename == "" || !c.allowFile(c.relPath(position.Filename)) {
		return
	}
	qualified := pkgPath + "." + owner + "." + method.Name()
	add(graphDeclaration{
		Kind: declarationMethod, Interface: isInterface,
		ID: ids.FunctionID(qualified), Filename: position.Filename,
		Line: position.Line, Column: position.Column,
		PackagePath: pkgPath, Owner: owner, Name: method.Name(),
	})
}

type declarationResolver struct {
	root  string
	files map[string][]graphDeclaration
}

func (r *declarationResolver) at(location gopls.Location) (graphDeclaration, bool, error) {
	filename, err := canonicalExistingPath(location.Filename)
	if err != nil {
		return graphDeclaration{}, false, err
	}
	root, err := canonicalExistingPath(r.root)
	if err != nil {
		return graphDeclaration{}, false, err
	}
	if !pathWithinRoot(root, filename) || pathContainsDir(filename, "vendor") {
		return graphDeclaration{}, false, nil
	}
	declarations, ok := r.files[filename]
	if !ok {
		declarations, err = declarationsInFile(root, filename)
		if err != nil {
			return graphDeclaration{}, false, err
		}
		r.files[filename] = declarations
	}
	for _, declaration := range declarations {
		if declaration.Line == location.Line && declaration.Column == location.Column {
			return declaration, true, nil
		}
	}
	return graphDeclaration{}, false, nil
}

func canonicalExistingPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return canonical, nil
}

func declarationsInFile(root, filename string) ([]graphDeclaration, error) {
	pkgPath, err := packagePathForFile(root, filename)
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filename, nil, 0)
	if err != nil {
		return nil, err
	}

	var declarations []graphDeclaration
	for _, declaration := range file.Decls {
		switch node := declaration.(type) {
		case *ast.GenDecl:
			if node.Tok != token.TYPE {
				continue
			}
			for _, spec := range node.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Assign.IsValid() {
					continue
				}
				_, isInterface := typeSpec.Type.(*ast.InterfaceType)
				position := fset.Position(typeSpec.Name.Pos())
				qualified := pkgPath + "." + typeSpec.Name.Name
				declarations = append(declarations, graphDeclaration{
					Kind: declarationType, Interface: isInterface, ID: ids.UnitID(qualified),
					Filename: filename, Line: position.Line, Column: position.Column,
					PackagePath: pkgPath, Owner: typeSpec.Name.Name, Name: typeSpec.Name.Name,
				})
				interfaceType, ok := typeSpec.Type.(*ast.InterfaceType)
				if !ok || interfaceType.Methods == nil {
					continue
				}
				for _, field := range interfaceType.Methods.List {
					for _, methodName := range field.Names {
						methodPosition := fset.Position(methodName.Pos())
						methodQualified := pkgPath + "." + typeSpec.Name.Name + "." + methodName.Name
						declarations = append(declarations, graphDeclaration{
							Kind: declarationMethod, Interface: true, ID: ids.FunctionID(methodQualified),
							Filename: filename, Line: methodPosition.Line, Column: methodPosition.Column,
							PackagePath: pkgPath, Owner: typeSpec.Name.Name, Name: methodName.Name,
						})
					}
				}
			}
		case *ast.FuncDecl:
			if node.Recv == nil || len(node.Recv.List) == 0 || node.Name == nil {
				continue
			}
			owner := baseIdent(typeExprString(node.Recv.List[0].Type))
			if owner == "" {
				continue
			}
			position := fset.Position(node.Name.Pos())
			qualified := pkgPath + "." + owner + "." + node.Name.Name
			declarations = append(declarations, graphDeclaration{
				Kind: declarationMethod, Interface: false, ID: ids.FunctionID(qualified),
				Filename: filename, Line: position.Line, Column: position.Column,
				PackagePath: pkgPath, Owner: owner, Name: node.Name.Name,
			})
		}
	}
	return declarations, nil
}

func packagePathForFile(root, filename string) (string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(filename)
	for {
		goMod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(goMod); err == nil {
			modulePath := modfile.ModulePath(data)
			if modulePath == "" {
				return "", errors.New("go.mod has no module path: " + goMod)
			}
			rel, err := filepath.Rel(dir, filepath.Dir(filename))
			if err != nil {
				return "", err
			}
			if rel == "." {
				return modulePath, nil
			}
			return strings.TrimSuffix(modulePath, "/") + "/" + filepath.ToSlash(rel), nil
		}
		if samePath(dir, root) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir || !pathWithinRoot(root, parent) {
			break
		}
		dir = parent
	}
	return "", errors.New("no go.mod for " + filename)
}

func pathWithinRoot(root, filename string) bool {
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	filename, err = filepath.Abs(filename)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, filename)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func pathContainsDir(filename, name string) bool {
	for _, part := range strings.Split(filepath.Clean(filename), string(filepath.Separator)) {
		if part == name {
			return true
		}
	}
	return false
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbs == rightAbs
}

func optionString(options map[string]any, key, fallback string) string {
	if value, ok := options[key].(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func optionBool(options map[string]any, key string, fallback bool) bool {
	if value, ok := options[key].(bool); ok {
		return value
	}
	return fallback
}

func optionInt(options map[string]any, key string, fallback int) int {
	switch value := options[key].(type) {
	case int:
		return value
	case float64: // encoding/json decodes numbers in map[string]any as float64.
		return int(value)
	default:
		return fallback
	}
}
