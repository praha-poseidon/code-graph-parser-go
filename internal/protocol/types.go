package protocol

// ParseRequest matches code-graph-parser-process stdin JSON (subset).
type ParseRequest struct {
	ProjectName    string                         `json:"projectName"`
	Language       string                         `json:"language"`
	ProjectRoot    string                         `json:"projectRoot"`
	SourceFiles    []string                       `json:"sourceFiles"`
	SourceRoots    []string                       `json:"sourceRoots"`
	RuleSources    []string                       `json:"ruleSources"`
	ExternalValues map[string]map[string][]string `json:"externalValues"`
	Options        map[string]any                 `json:"options"`
}

type GraphDelta struct {
	Scope         DeltaScope         `json:"scope"`
	Packages      []CodePackage      `json:"packages"`
	Units         []CodeUnit         `json:"units"`
	Functions     []CodeFunction     `json:"functions"`
	Endpoints     []map[string]any   `json:"endpoints"`
	Relationships []CodeRelationship `json:"relationships"`
	DeletedNodeIds []string          `json:"deletedNodeIds"`
	DeletedRelationshipIds []string  `json:"deletedRelationshipIds"`
	Diagnostics   []Diagnostic       `json:"diagnostics"`
}

type DeltaScope struct {
	ProjectName string   `json:"projectName"`
	Language    string   `json:"language"`
	ProjectRoot string   `json:"projectRoot"`
	SourceFiles []string `json:"sourceFiles"`
}

type CodePackage struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Language        string `json:"language"`
	ProjectFilePath string `json:"projectFilePath"`
	ProjectName     string `json:"projectName"`
}

type CodeUnit struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Language        string `json:"language"`
	ProjectFilePath string `json:"projectFilePath"`
	ProjectName     string `json:"projectName"`
	UnitKind        string `json:"unitKind,omitempty"`
}

type CodeFunction struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Language        string `json:"language"`
	ProjectFilePath string `json:"projectFilePath"`
	ProjectName     string `json:"projectName"`
	Signature       string `json:"signature,omitempty"`
	StartLine       int    `json:"startLine,omitempty"`
	EndLine         int    `json:"endLine,omitempty"`
}

type CodeRelationship struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	FromNodeID       string `json:"fromNodeId"`
	ToNodeID         string `json:"toNodeId"`
	Language         string `json:"language"`
	ProjectFilePath  string `json:"projectFilePath"`
	ProjectName      string `json:"projectName"`
}

type Diagnostic struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
