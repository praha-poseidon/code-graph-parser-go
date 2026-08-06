// Package ids matches code-graph-model GraphIds conventions (raw ids, no project scope).
package ids

import (
	"crypto/sha1"
	"encoding/hex"
	"strings"
)

func PackageID(packageName string) string {
	return "pkg:" + normalize(packageName)
}

func UnitID(qualifiedName string) string {
	return "unit:" + normalize(qualifiedName)
}

func FunctionID(qualifiedSignature string) string {
	return "fn:" + normalize(qualifiedSignature)
}

func EndpointID(direction, endpointType, matchIdentity string) string {
	return "endpoint:" + normalize(direction) + ":" + normalize(endpointType) + ":" + sha1Hex(normalize(matchIdentity))
}

func RelationshipID(fromNodeID, relType, toNodeID string) string {
	return "rel:" + sha1Hex(normalize(fromNodeID)+"|"+normalize(relType)+"|"+normalize(toNodeID))
}

func PlaceholderFunctionID(qualifiedSignature string) string {
	return "placeholder:" + FunctionID(qualifiedSignature)
}

func normalize(value string) string {
	return strings.TrimSpace(value)
}

func sha1Hex(value string) string {
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}
