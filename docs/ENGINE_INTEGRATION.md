# 接入 code-graph-engine

## 边界（必须分清）

| 组件 | 职责 |
|------|------|
| **code-graph-parser-go** | 收到引擎的**增量** `ParseRequest`，解析相关 Go 源码，返回**这一次**的 `GraphDelta`（packages/units/functions/relationships/endpoints） |
| **code-graph-parser-process** | Java 起子进程、stdin/stdout 传协议 |
| **code-graph-core / engine** | **合并、写入、删除、级联**；`SOURCE_DELETED` 等由引擎处理器处理，**不要求 parser 算删除列表** |

Parser 输出里的 `deletedNodeIds` / `deletedRelationshipIds` **固定为空数组**。  
删除文件时引擎走 `RemovedSourceProcessor` 等逻辑，不是 Go parser 的活。

与 **code-graph-parser-js** 相同分工。

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
