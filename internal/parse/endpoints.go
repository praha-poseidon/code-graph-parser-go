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
		direction := firstNonEmpty(f.Fields["direction"], f.Classifiers["direction"])
		if direction == "" {
			direction = "inbound"
		}
		epType := strings.ToUpper(firstNonEmpty(f.Fields["endpointType"], f.Classifiers["category"]))
		if epType == "" {
			epType = "HTTP"
		}
		identityValue := endpointIdentityValue(epType, f.Fields)
		if identityValue == "" {
			continue
		}
		matchIdentity := epType + ":" + identityValue
		if epType == "HTTP" {
			method = strings.ToUpper(strings.TrimSpace(method))
			if method == "" {
				method = "ANY"
			}
			matchIdentity = "HTTP:" + method + ":" + path
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
			"isExternal":      direction == "outbound",
			"parseLevel":      firstNonEmpty(f.Fields["parseLevel"], "full"),
			"httpMethod":      optionalWhen(epType == "HTTP", method),
			"path":            optionalWhen(epType == "HTTP", identityValue),
			"normalizedPath":  optionalWhen(epType == "HTTP", identityValue),
			"topic":           optionalString(f.Fields["topic"]),
			"group":           optionalString(f.Fields["group"]),
			"operation":       optionalString(f.Fields["operation"]),
			"brokerType":      optionalString(f.Fields["brokerType"]),
			"keyPattern":      optionalString(firstNonEmpty(f.Fields["keyPattern"], f.Fields["key"])),
			"command":         optionalString(canonicalCommand(f.Fields["command"])),
			"dataStructure":   optionalString(f.Fields["dataStructure"]),
			"tableName":       optionalString(firstNonEmpty(f.Fields["tableName"], f.Fields["table"])),
			"dbOperation":     optionalString(firstNonEmpty(f.Fields["dbOperation"], f.Fields["operation"])),
			"matchIdentity":   matchIdentity,
			"startLine":       f.StartLine,
			"endLine":         f.EndLine,
			"qualifiedName":   matchIdentity,
			"other":           optionalString(f.Fields["other"]),
		}
		c.Delta.Endpoints = append(c.Delta.Endpoints, ep)

		// Link endpoint ↔ function
		fnID := endpointHandlerFunction(c, f)
		if fnID == "" {
			fnID = handlerByFileLine[f.ProjectFilePath+":"+itoa(f.StartLine)]
		}
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

func endpointIdentityValue(endpointType string, fields map[string]string) string {
	switch endpointType {
	case "HTTP":
		return strings.TrimSpace(firstNonEmpty(fields["path"], fields["url"], fields["route"]))
	case "MQ":
		return strings.TrimSpace(fields["topic"])
	case "REDIS":
		return strings.TrimSpace(firstNonEmpty(fields["keyPattern"], fields["key"]))
	case "DB":
		return strings.TrimSpace(firstNonEmpty(fields["tableName"], fields["table"]))
	default:
		return strings.TrimSpace(firstNonEmpty(fields["path"], fields["topic"], fields["keyPattern"], fields["key"], fields["tableName"], fields["table"]))
	}
}

func canonicalCommand(command string) string {
	value := strings.ToUpper(strings.TrimSpace(command))
	if value == "DEL" {
		return "DELETE"
	}
	return value
}

func endpointHandlerFunction(c *Context, fact extractapi.Fact) string {
	match := func(function protocol.CodeFunction, symbol string) bool {
		return function.ProjectFilePath == fact.ProjectFilePath &&
			(function.QualifiedName == symbol || strings.HasSuffix(function.QualifiedName, "."+symbol))
	}
	if handler := strings.TrimSpace(fact.Fields["handler"]); handler != "" {
		var found string
		for _, function := range c.Delta.Functions {
			if function.ProjectFilePath == fact.ProjectFilePath && function.Name == handler {
				if found != "" {
					return ""
				}
				found = function.ID
			}
		}
		return found
	}
	if symbol := strings.TrimSpace(fact.EnclosingSymbol); symbol != "" {
		for _, function := range c.Delta.Functions {
			if match(function, symbol) {
				return function.ID
			}
		}
	}
	return ""
}

func optionalString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func optionalWhen(include bool, value string) any {
	if !include {
		return nil
	}
	return optionalString(value)
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
