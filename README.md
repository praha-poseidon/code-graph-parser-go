# code-graph-parser-go

Go language **process parser** for [code-graph-engine](https://github.com/praha-poseidon/code-graph-engine).

Same role as **code-graph-parser-js**: Java invokes this CLI, receives **GraphDelta**, and **writes the graph itself**.

```text
code-graph-parser-process (Java)
    stdin  → ParseRequest JSON
    stdout ← GraphDelta JSON
         │
         ▼
code-graph-parser-go --stdio
    go/packages (AST + types)
    nodes + relationships (+ endpoints via SER)
```

## Build / test

```bash
export PATH="$HOME/.local/go/bin:$PATH"
go test ./...
go build -o bin/code-graph-parser-go ./cmd/code-graph-parser-go
./scripts/smoke-stdio.sh
```

## Engine config

```bash
-Dcodegraph.parser.process.languages=go
-Dcodegraph.parser.process.go.command="/abs/path/code-graph-parser-go --stdio"
```

## GraphDelta coverage

| Item | Status |
|------|--------|
| CodePackage | yes |
| CodeUnit (struct / interface / package synthetic) | yes |
| CodeFunction (incl. interface methods, external placeholders) | yes |
| PACKAGE_TO_UNIT | yes |
| UNIT_TO_FUNCTION | yes |
| CALLS (types.Info + placeholders) | yes |
| EXTENDS (struct/interface embed) | yes |
| IMPLEMENTS (`types.Implements`) | yes |
| OVERRIDES (interface methods + embed shadow) | yes |
| Endpoints (`ruleSources` → static-extract-go) | yes |
| ENDPOINT_TO_FUNCTION / FUNCTION_TO_ENDPOINT | yes |
| sourceFiles filter (incremental emit) | yes |
| MATCHES (cross-service) | engine-side |

IDs / `relationshipType` names match Java `GraphIds` + `RelationshipType` enum.

## Layout

```text
cmd/code-graph-parser-go/     CLI --stdio
internal/protocol/            ParseRequest / GraphDelta
internal/ids/                 GraphIds-compatible
internal/load/                go/packages
internal/parse/
  parse.go                    pipeline orchestrator
  nodes.go                    packages / units / functions
  calls.go                    CALLS + placeholders
  inheritance.go              EXTENDS (embed)
  implements.go               IMPLEMENTS
  overrides.go                OVERRIDES
  endpoints.go                SER endpoints + links
testdata/                     module, embed, iface fixtures
```

## Optional endpoints

Pass SER in `ParseRequest.ruleSources` (or `--rule` in debug). Without SER you still get the full structure graph.
