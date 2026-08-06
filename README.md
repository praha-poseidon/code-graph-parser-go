# code-graph-parser-go

Go language **process parser** for [code-graph-engine](https://github.com/praha-poseidon/code-graph-engine).

Same role as **code-graph-parser-js**:

```text
引擎发增量 ParseRequest（含 sourceFiles / changeType）
        │
        ▼
code-graph-parser-go --stdio
  只解析这次相关源码 → 产出本次 GraphDelta（节点+关系）
  deletedNodeIds / deletedRelationshipIds 保持空（删除由引擎做）
        │
        ▼
code-graph-engine 合并/落库/删除/级联
```

**Parser 不做图存储，不做删除策略。** 引擎自己根据变更类型应用 delta。

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
| 按 `sourceFiles` 产出本次增量涉及的节点 | yes（类型解析仍 load 模块） |
| 删除节点/关系 | **否 — 引擎**（`SOURCE_DELETED` 等） |
| MATCHES 跨服务匹配 | **否 — 引擎** |

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
