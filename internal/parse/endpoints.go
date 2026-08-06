package parse

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/praha-poseidon/code-graph-parser-go/internal/ids"
	"github.com/praha-poseidon/static-extract-go/extractapi"
)

// attachEndpoints runs static-extract-go when ruleSources are present and maps facts → endpoints.
// This is optional enrichment; structure graph works without SER.
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
	for _, f := range facts {
		method := f.Fields["httpMethod"]
		if method == "" {
			method = f.Fields["method"]
		}
		path := f.Fields["path"]
		if path == "" {
			path = f.Fields["url"]
		}
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
		id := ids.EndpointID(direction, epType, matchIdentity+"|"+fmt.Sprint(f.StartLine))
		// Prefer process-README style when identity is clean:
		if method != "" && path != "" {
			sum := sha1.Sum([]byte(matchIdentity))
			_ = hex.EncodeToString(sum[:])
		}
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
			"matchIdentity":   matchIdentity,
			"startLine":       f.StartLine,
			"endLine":         f.EndLine,
			"qualifiedName":   matchIdentity,
		}
		c.Delta.Endpoints = append(c.Delta.Endpoints, ep)

		// ENDPOINT_TO_FUNCTION when we can resolve enclosing handler by line (best-effort skip)
		// Outbound FUNCTION_TO_ENDPOINT similarly deferred unless fields include function id.
	}
	return nil
}
