// Package protocol mirrors code-graph-engine ParseRequest / GraphDelta JSON
// for the process-parser CLI (same contract as code-graph-parser-js).
package protocol

// ParseRequest is stdin JSON from code-graph-parser-process.
type ParseRequest struct {
	ProjectName    string                         `json:"projectName"`
	Language       string                         `json:"language"`
	ProjectRoot    string                         `json:"projectRoot"`
	SourceFiles    []string                       `json:"sourceFiles"`
	SourceRoots    []string                       `json:"sourceRoots"`
	Dependencies   []string                       `json:"dependencies"`
	GitRepoURL     string                         `json:"gitRepoUrl"`
	GitBranch      string                         `json:"gitBranch"`
	ChangeType     string                         `json:"changeType"`
	RuleSources    []string                       `json:"ruleSources"`
	ExternalValues map[string]map[string][]string `json:"externalValues"`
	Options        map[string]any                 `json:"options"`
}

// GraphDelta is stdout JSON consumed by the Java engine.
type GraphDelta struct {
	Scope         DeltaScope         `json:"scope"`
	Packages      []CodePackage      `json:"packages"`
	Units         []CodeUnit         `json:"units"`
	Functions     []CodeFunction     `json:"functions"`
	Endpoints     []map[string]any   `json:"endpoints"`
	Relationships []CodeRelationship `json:"relationships"`
	// Deleted* must remain empty from this parser. The engine owns delete/cascade.
	DeletedNodeIds         []string     `json:"deletedNodeIds"`
	DeletedRelationshipIds []string     `json:"deletedRelationshipIds"`
	Diagnostics            []Diagnostic `json:"diagnostics"`
}

type DeltaScope struct {
	ProjectName string         `json:"projectName"`
	Language    string         `json:"language"`
	GitRepoURL  string         `json:"gitRepoUrl,omitempty"`
	GitBranch   string         `json:"gitBranch,omitempty"`
	ProjectRoot string         `json:"projectRoot"`
	SourceFiles []string       `json:"sourceFiles"`
	ChangeType  string         `json:"changeType,omitempty"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}

// Shared relationship names are cross-language protocol vocabulary. Go-specific
// structural names stay native and are classified through RelationshipKind.
const (
	RelCalls              = "CALLS"
	RelPackageToUnit      = "PACKAGE_TO_UNIT"
	RelUnitToFunction     = "UNIT_TO_FUNCTION"
	RelEmbeds             = "GO_EMBEDS"
	RelSatisfies          = "GO_SATISFIES"
	RelMethodSatisfies    = "GO_METHOD_SATISFIES"
	RelEndpointToFunc     = "ENDPOINT_TO_FUNCTION"
	RelFunctionToEndpoint = "FUNCTION_TO_ENDPOINT"
)

type CodePackage struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	QualifiedName   string `json:"qualifiedName"`
	Language        string `json:"language"`
	ProjectName     string `json:"projectName"`
	ProjectFilePath string `json:"projectFilePath"`
	GitRepoURL      string `json:"gitRepoUrl,omitempty"`
	GitBranch       string `json:"gitBranch,omitempty"`
	PackagePath     string `json:"packagePath,omitempty"`
	StartLine       *int   `json:"startLine,omitempty"`
	EndLine         *int   `json:"endLine,omitempty"`
}

type CodeUnit struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	QualifiedName   string   `json:"qualifiedName"`
	Language        string   `json:"language"`
	ProjectName     string   `json:"projectName"`
	ProjectFilePath string   `json:"projectFilePath"`
	GitRepoURL      string   `json:"gitRepoUrl,omitempty"`
	GitBranch       string   `json:"gitBranch,omitempty"`
	UnitType        string   `json:"unitType"` // struct, interface, type alias → map to class/interface
	Modifiers       []string `json:"modifiers,omitempty"`
	IsAbstract      *bool    `json:"isAbstract,omitempty"`
	PackageID       string   `json:"packageId,omitempty"`
	StartLine       *int     `json:"startLine,omitempty"`
	EndLine         *int     `json:"endLine,omitempty"`
}

type CodeFunction struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	QualifiedName   string   `json:"qualifiedName"`
	Language        string   `json:"language"`
	ProjectName     string   `json:"projectName"`
	ProjectFilePath string   `json:"projectFilePath"`
	GitRepoURL      string   `json:"gitRepoUrl,omitempty"`
	GitBranch       string   `json:"gitBranch,omitempty"`
	Signature       string   `json:"signature,omitempty"`
	ReturnType      string   `json:"returnType,omitempty"`
	Modifiers       []string `json:"modifiers,omitempty"`
	IsStatic        *bool    `json:"isStatic,omitempty"`
	IsConstructor   *bool    `json:"isConstructor,omitempty"`
	IsPlaceholder   *bool    `json:"isPlaceholder,omitempty"`
	StartLine       *int     `json:"startLine,omitempty"`
	EndLine         *int     `json:"endLine,omitempty"`
}

type CodeRelationship struct {
	ID               string `json:"id"`
	FromNodeID       string `json:"fromNodeId"`
	ToNodeID         string `json:"toNodeId"`
	RelationshipType string `json:"relationshipType"`
	RelationshipKind string `json:"relationshipKind"`
	FromNodeType     string `json:"fromNodeType"`
	ToNodeType       string `json:"toNodeType"`
	LineNumber       *int   `json:"lineNumber,omitempty"`
	CallType         string `json:"callType,omitempty"`
	Language         string `json:"language"`
	ProjectName      string `json:"projectName"`
}

func RelationshipContract(relationshipType string) (kind, fromNodeType, toNodeType string) {
	switch relationshipType {
	case RelCalls:
		return "CALL", "CodeFunction", "CodeFunction"
	case RelPackageToUnit:
		return "CONTAINS", "CodePackage", "CodeUnit"
	case RelUnitToFunction:
		return "CONTAINS", "CodeUnit", "CodeFunction"
	case RelEmbeds:
		return "EMBEDS", "CodeUnit", "CodeUnit"
	case RelSatisfies:
		return "CONFORMS", "CodeUnit", "CodeUnit"
	case RelMethodSatisfies:
		return "REFINES", "CodeFunction", "CodeFunction"
	case RelEndpointToFunc:
		return "BINDS_ENDPOINT", "CodeEndpoint", "CodeFunction"
	case RelFunctionToEndpoint:
		return "BINDS_ENDPOINT", "CodeFunction", "CodeEndpoint"
	default:
		return "", "", ""
	}
}

type Diagnostic struct {
	Level           string         `json:"level,omitempty"`
	Code            string         `json:"code,omitempty"`
	Message         string         `json:"message"`
	ProjectFilePath string         `json:"projectFilePath,omitempty"`
	LineNumber      *int           `json:"lineNumber,omitempty"`
	Details         map[string]any `json:"details"`
}

func EmptyDelta(req ParseRequest) GraphDelta {
	return GraphDelta{
		Scope: DeltaScope{
			ProjectName: req.ProjectName,
			Language:    "go",
			GitRepoURL:  req.GitRepoURL,
			GitBranch:   req.GitBranch,
			ProjectRoot: req.ProjectRoot,
			SourceFiles: req.SourceFiles,
			ChangeType:  req.ChangeType,
			Attributes:  map[string]any{},
		},
		Packages:               []CodePackage{},
		Units:                  []CodeUnit{},
		Functions:              []CodeFunction{},
		Endpoints:              []map[string]any{},
		Relationships:          []CodeRelationship{},
		DeletedNodeIds:         []string{},
		DeletedRelationshipIds: []string{},
		Diagnostics:            []Diagnostic{},
	}
}
