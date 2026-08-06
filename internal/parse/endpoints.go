package parse

import (
	"fmt"
	"go/ast"
	"go/types"
	"strings"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/code-graph-parser-go/internal/protocol"
	"github.com/praha-poseidon/static-extract-go/extractapi"
	"golang.org/x/tools/go/packages"
)

// attachEndpoints runs SER rules and links endpoints to functions.
func attachEndpoints(c *Context) error {
	if len(c.Req.RuleSources) == 0 {
		return nil
	}
	facts, err := extractapi.Run(extractapi.Request{
		ProjectRoot:    c.Root,
		Packages:       c.Pkgs,
		RuleSources:    c.Req.RuleSources,
		ExternalValues: c.Req.ExternalValues,
	})
	if err != nil {
		return err
	}

	// Precompute HandleFunc-style call sites: line -> handler function id
	handlerByFileLine := map[string]string{}
	for _, pkg := range c.Pkgs {
		for _, file := range pkg.Syntax {
			rel := c.relPath(pkg.Fset.Position(file.Pos()).Filename)
			ast.Inspect(file, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				name, _ := callNameSimple(ce)
				// common route registrations
				if name != "HandleFunc" && name != "Handle" && name != "GET" && name != "POST" &&
					name != "PUT" && name != "DELETE" && name != "PATCH" && name != "Any" && name != "Group" {
					// still try last arg as handler for HandleFunc-like
					if !looksLikeRouteCall(ce, pkg) {
						return true
					}
				}
				line := pkg.Fset.Position(ce.Pos()).Line
				if fnID := resolveHandlerArg(c, pkg, ce); fnID != "" {
					handlerByFileLine[rel+":"+itoa(line)] = fnID
				}
				return true
			})
		}
	}

	for _, f := range facts {
		method := firstNonEmpty(f.Fields["httpMethod"], f.Fields["method"])
		path := firstNonEmpty(f.Fields["path"], f.Fields["url"])
		direction := f.Classifiers["direction"]
		if direction == "" {
			direction = "inbound"
		}
		epType := strings.ToUpper(f.Classifiers["category"])
		if epType == "" {
			epType = "HTTP"
		}
		matchIdentity := strings.TrimSpace(method + " " + path)
		if matchIdentity == "" {
			matchIdentity = f.Rule
		}
		id := ids.EndpointID(direction, epType, matchIdentity+"|"+f.ProjectFilePath+"|"+fmt.Sprint(f.StartLine))
		ep := map[string]any{
			"endpointKind":    strings.ToLower(epType),
			"id":              id,
			"name":            matchIdentity,
			"language":        "go",
			"projectName":     c.projectName(),
			"projectFilePath": f.ProjectFilePath,
			"endpointType":    epType,
			"direction":       direction,
			"httpMethod":      method,
			"path":            path,
			"normalizedPath":  path,
			"matchIdentity":   matchIdentity,
			"startLine":       f.StartLine,
			"endLine":         f.EndLine,
			"qualifiedName":   matchIdentity,
		}
		c.Delta.Endpoints = append(c.Delta.Endpoints, ep)

		// Link endpoint ↔ function
		fnID := handlerByFileLine[f.ProjectFilePath+":"+itoa(f.StartLine)]
		if fnID == "" {
			// try nearby lines
			for d := 0; d <= 2 && fnID == ""; d++ {
				fnID = handlerByFileLine[f.ProjectFilePath+":"+itoa(f.StartLine+d)]
				if fnID == "" {
					fnID = handlerByFileLine[f.ProjectFilePath+":"+itoa(f.StartLine-d)]
				}
			}
		}
		if fnID == "" {
			// enclosing function of the registration site
			fnID = c.FuncAtFileLine[f.ProjectFilePath+":"+itoa(f.StartLine)]
		}
		if fnID == "" {
			continue
		}
		if direction == "inbound" {
			c.addRel(protocol.CodeRelationship{
				ID:               ids.RelationshipID(id, protocol.RelEndpointToFunc, fnID),
				FromNodeID:       id,
				ToNodeID:         fnID,
				RelationshipType: protocol.RelEndpointToFunc,
				Language:         "go",
				ProjectName:      c.projectName(),
				LineNumber:       intPtr(f.StartLine),
			})
		} else {
			c.addRel(protocol.CodeRelationship{
				ID:               ids.RelationshipID(fnID, protocol.RelFunctionToEndpoint, id),
				FromNodeID:       fnID,
				ToNodeID:         id,
				RelationshipType: protocol.RelFunctionToEndpoint,
				Language:         "go",
				ProjectName:      c.projectName(),
				LineNumber:       intPtr(f.StartLine),
			})
		}
	}
	return nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func callNameSimple(ce *ast.CallExpr) (name, recv string) {
	switch f := ce.Fun.(type) {
	case *ast.Ident:
		return f.Name, ""
	case *ast.SelectorExpr:
		return f.Sel.Name, ""
	default:
		return "", ""
	}
}

func looksLikeRouteCall(ce *ast.CallExpr, pkg *packages.Package) bool {
	if len(ce.Args) < 2 {
		return false
	}
	// first arg string literal path-like
	if bl, ok := ce.Args[0].(*ast.BasicLit); ok && bl.Kind.String() == "STRING" {
		return true
	}
	return false
}

func resolveHandlerArg(c *Context, pkg *packages.Package, ce *ast.CallExpr) string {
	if len(ce.Args) == 0 {
		return ""
	}
	// handler is usually last arg
	arg := ce.Args[len(ce.Args)-1]
	info := pkg.TypesInfo
	switch a := arg.(type) {
	case *ast.Ident:
		if info != nil {
			if obj := info.Uses[a]; obj != nil {
				if fn, ok := obj.(*types.Func); ok {
					return typesFuncID(fn)
				}
			}
		}
		// same package function
		q := pkg.PkgPath + "." + a.Name
		if id, ok := c.FuncByQName[q]; ok {
			return id
		}
		return ids.FunctionID(q)
	case *ast.SelectorExpr:
		if info != nil {
			if obj := info.Uses[a.Sel]; obj != nil {
				if fn, ok := obj.(*types.Func); ok {
					return typesFuncID(fn)
				}
			}
		}
	case *ast.FuncLit:
		// anonymous — no stable function node; skip
		return ""
	}
	return ""
}
