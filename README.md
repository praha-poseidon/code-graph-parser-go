# code-graph-parser-go

Go language **process parser** for [code-graph-engine](https://github.com/praha-poseidon/code-graph-engine).

Same role as **code-graph-parser-js**:

```text
引擎发增量 ParseRequest（含 sourceFiles / changeType）
        │
        ▼
parser-go --stdio
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
go build -o bin/parser-go ./cmd/parser-go
./scripts/smoke-stdio.sh
```

## Engine config

```bash
-Dcodegraph.parser.process.languages=go
-Dcodegraph.parser.process.go.command="/abs/path/parser-go --stdio-stream"
-Dcodegraph.parser.process.go.streaming=true
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
| GO_EMBEDS (struct/interface embed) | yes |
| GO_SATISFIES (`types.Implements`) | yes |
| GO_METHOD_SATISFIES (interface methods + embed shadow) | yes |
| Endpoints (`ruleSources` → static-extract-go) | yes |
| ENDPOINT_TO_FUNCTION / FUNCTION_TO_ENDPOINT | yes |
| 按 `sourceFiles` 产出本次增量涉及的节点 | yes（只 load 当前 package + 必要依赖） |
| 删除节点/关系 | **否 — 引擎**（`SOURCE_DELETED` 等） |
| MATCHES 跨服务匹配 | **否 — 引擎** |

## Go incremental implementation lookup (gopls)

Incremental requests (`sourceFiles` is non-empty) load only the package that
contains the changed file, using the `go/packages` `file=<path>` query. Global
implicit interface relationships are supplied by the official `gopls` LSP
`textDocument/implementation` request.

Install `gopls` beside the Go toolchain used by the parser:

```bash
# This version is compatible with the repository's Go 1.23.6 toolchain.
go install golang.org/x/tools/gopls@v0.18.1
```

When the deployment toolchain is upgraded, pin a newer compatible gopls
version instead of using an unpinned `@latest` during packaging.
The supplied wrapper includes both `~/.local/go/bin` and the default
`~/go/bin` in `PATH`.

All sequential file requests for the same cloned project share this default
task-local cache:

```text
<projectRoot>/.codegraph-cache/gopls
```

Deleting the cloned project therefore deletes the gopls cache too. The Engine
opens one task-local parser session before its sequential file loop. That
session owns one `gopls serve` process and reuses the same in-memory LSP
snapshot and disk cache for every file. Closing the task session stops both
the parser and gopls processes before the cloned project is deleted.

Optional `ParseRequest.options`:

| option | default | purpose |
| --- | --- | --- |
| `goplsEnabled` | `true` for incremental requests | enable global implementation lookup |
| `goplsCommand` | `CODEGRAPH_GOPLS_COMMAND` or `gopls` | executable path |
| `goplsCacheDir` | `<projectRoot>/.codegraph-cache/gopls` | task-local shared cache directory |
| `goplsConcurrency` | `4` | maximum parallel LSP requests inside the task-local gopls process |
| `goplsRequired` | `false` | return an error diagnostic when gopls is unavailable |

When gopls is unavailable and not required, the parser keeps local-package
`go/types` relationships and sets `scope.attributes.goplsImplementation` to
`unavailable`.

IDs / `relationshipType` names match Java `GraphIds` + `RelationshipType` enum.

## Layout

```text
cmd/parser-go/                CLI --stdio
internal/protocol/            ParseRequest / GraphDelta
internal/ids/                 GraphIds-compatible
internal/load/                go/packages
internal/gopls/               task-local reusable LSP implementation client
internal/parse/
  parse.go                    pipeline orchestrator
  nodes.go                    packages / units / functions
  calls.go                    CALLS + placeholders
  inheritance.go              GO_EMBEDS
  implements.go               GO_SATISFIES
  overrides.go                GO_METHOD_SATISFIES
  endpoints.go                SER endpoints + links
testdata/                     module, embed, iface fixtures
```

## Optional endpoints

Pass SER in `ParseRequest.ruleSources` (or `--rule` in debug). Without SER you still get the full structure graph.

SER method dictionaries use canonical `import/path.Owner.Method()` keys and
support `key`, `key.1`, `key.2`, … fan-out. Endpoint materialization supports
HTTP, MQ, REDIS, and DB identities, preserves nullable `other`, and skips facts
whose required identity is still empty after all `from`/`fallback` sources.
