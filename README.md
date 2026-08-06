# code-graph-parser-go

Go language **process parser** for [code-graph-engine](https://github.com/praha-poseidon/code-graph-engine).

Same role as **code-graph-parser-js**:

```text
Java engine (code-graph-parser-process)
    │  stdin:  ParseRequest JSON
    │  stdout: GraphDelta JSON
    ▼
code-graph-parser-go --stdio
    │  go/packages (AST + types)
    ▼
nodes + relationships (+ optional endpoints via SER)
```

Java **does not** parse Go. This CLI produces the graph delta; the engine writes storage.

## Build

```bash
export PATH="$HOME/.local/go/bin:$PATH"   # if needed
go test ./...
go build -o bin/code-graph-parser-go ./cmd/code-graph-parser-go
```

## Process protocol (engine)

```bash
-Dcodegraph.parser.process.languages=go
-Dcodegraph.parser.process.go.command="/abs/path/code-graph-parser-go --stdio"
```

## Debug

```bash
./bin/code-graph-parser-go --project testdata/module
./bin/code-graph-parser-go --project ../static-extract-go/examples/conformance/http-handlefunc \
  --rule ../static-extract-go/examples/conformance/http-handlefunc/rule.ser
./scripts/smoke-stdio.sh
```

## GraphDelta coverage (current)

| Kind | Status |
|------|--------|
| CodePackage | yes |
| CodeUnit (struct/interface + package-level synthetic) | yes |
| CodeFunction | yes |
| PACKAGE_TO_UNIT | yes |
| UNIT_TO_FUNCTION | yes |
| CALLS (types.Info) | yes |
| EXTENDS (struct/interface embed) | yes |
| IMPLEMENTS / OVERRIDES | not yet |
| Endpoints via `ruleSources` → static-extract-go | yes |
| ENDPOINT_TO_FUNCTION | not yet |

IDs follow engine `GraphIds` prefixes (`pkg:`, `unit:`, `fn:`, `rel:`). Relationship type strings match Java `RelationshipType` enum names.

## Layout

```text
cmd/code-graph-parser-go/   CLI --stdio
internal/protocol/          ParseRequest / GraphDelta JSON
internal/ids/               GraphIds-compatible ids
internal/load/              go/packages
internal/parse/             pipeline: nodes → calls → embed → endpoints
testdata/                   modules for tests
docs/ENGINE_INTEGRATION.md
```
