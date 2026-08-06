# 接入 code-graph-engine

## 进程协议

与 `code-graph-parser-process` 一致：

1. 启动：`code-graph-parser-go --stdio`
2. stdin：一个 `ParseRequest` JSON（单次请求后进程可退出；若 adapter 复用进程则按其约定）
3. stdout：一个 `GraphDelta` JSON

## JVM 配置示例

```bash
-Dcodegraph.parser.process.languages=go
-Dcodegraph.parser.process.go.command="/abs/path/to/code-graph-parser-go --stdio"
-Dcodegraph.parser.process.timeoutSeconds=120
```

或环境变量：

```bash
CODEGRAPH_PARSER_PROCESS_LANGUAGES=go
CODEGRAPH_PARSER_GO_COMMAND="/abs/path/to/code-graph-parser-go --stdio"
```

## ParseRequest 字段（Go 关注）

| 字段 | 用途 |
|------|------|
| `projectRoot` | module 根（`go/packages` Dir） |
| `projectName` | 写入节点 projectName |
| `language` | 固定 `go` |
| `ruleSources` | SER 字符串列表 → endpoints |
| `externalValues` | 字典（trace） |
| `sourceFiles` | 可选；当前 v1 仍 Load `./...` 保证类型 |

## 冒烟

```bash
export PATH="$HOME/.local/go/bin:$PATH"
cd code-graph-parser-go
go build -o bin/code-graph-parser-go ./cmd/code-graph-parser-go

ROOT="$(cd ../static-extract-go/examples/conformance/http-handlefunc && pwd)"
SER=$(python3 -c "import json; print(json.dumps(open('$ROOT/rule.ser').read()))")
printf '%s' "{\"projectName\":\"demo\",\"language\":\"go\",\"projectRoot\":\"$ROOT\",\"ruleSources\":[$SER]}" \
  | ./bin/code-graph-parser-go --stdio | jq '.endpoints | length'
# expect 2
```
