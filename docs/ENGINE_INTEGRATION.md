# 接入 code-graph-engine

## 边界

| 组件 | 职责 |
|------|------|
| **code-graph-parser-go** | 解析 Go → GraphDelta（节点+关系） |
| **code-graph-parser-process** | Java 起进程、传 ParseRequest、读 GraphDelta |
| **code-graph-core** | 写图存储 |

与 **code-graph-parser-js** 相同。

## 配置

```bash
-Dcodegraph.parser.process.languages=go
-Dcodegraph.parser.process.go.command="/abs/path/to/code-graph-parser-go --stdio"
-Dcodegraph.parser.process.timeoutSeconds=120
```

## 协议

- stdin：一个 `ParseRequest` JSON  
- stdout：一个 `GraphDelta` JSON  
- 非 0 退出时 stderr 为诊断  

字段对齐 engine model：`relationshipType`、`qualifiedName`、`projectFilePath`、`projectName`、`language` 等。

## 可选端点

`ParseRequest.ruleSources` 传入 SER 字符串时，parser 内部调用 **static-extract-go** 填充 `endpoints`。  
不传则只有结构图。

## 冒烟

```bash
./scripts/smoke-stdio.sh
```
