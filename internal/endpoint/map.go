package endpoint

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/praha-poseidon/static-extract-go/extractapi"
)

// ToGraphEndpoints maps static-extract facts into GraphDelta endpoint objects.
func ToGraphEndpoints(projectName string, facts []extractapi.Fact) []map[string]any {
	var out []map[string]any
	for _, f := range facts {
		epType := strings.ToUpper(f.Classifiers["category"])
		if epType == "" {
			epType = "HTTP"
		}
		direction := f.Classifiers["direction"]
		if direction == "" {
			direction = "inbound"
		}
		method := f.Fields["httpMethod"]
		if method == "" {
			method = f.Fields["method"]
		}
		path := f.Fields["path"]
		if path == "" {
			path = f.Fields["url"]
		}
		key := fmt.Sprintf("%s|%s|%s|%s|%d", direction, epType, method, path, f.StartLine)
		sum := sha1.Sum([]byte(key))
		id := "endpoint:" + hex.EncodeToString(sum[:10])
		name := strings.TrimSpace(method + " " + path)
		if name == "" {
			name = f.Rule
		}
		out = append(out, map[string]any{
			"endpointKind":    strings.ToLower(epType),
			"id":              id,
			"name":            name,
			"language":        "go",
			"projectFilePath": f.ProjectFilePath,
			"projectName":     projectName,
			"endpointType":    epType,
			"direction":       direction,
			"httpMethod":      method,
			"path":            path,
			"startLine":       f.StartLine,
			"endLine":         f.EndLine,
			"fields":          f.Fields,
			"rule":            f.Rule,
			"factType":        f.FactType,
		})
	}
	return out
}
