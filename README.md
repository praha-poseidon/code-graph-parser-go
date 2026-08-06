# code-graph-parser-go

Go project → `GraphDelta` for [code-graph-engine](../code-graph-engine) via process protocol.

## Stack

- **AST**: `golang.org/x/tools/go/packages`
- **Endpoints**: [static-extract-go](../static-extract-go) (SER `ruleSources` on ParseRequest)

## Build

```bash
export PATH="$HOME/.local/go/bin:$PATH"
cd code-graph-parser-go
go test ./...
go build -o bin/code-graph-parser-go ./cmd/code-graph-parser-go
```

## Debug

```bash
# structure only
./bin/code-graph-parser-go --project testdata/module

# structure + endpoints from SER file
./bin/code-graph-parser-go --project ../static-extract-go/examples/conformance/http-handlefunc \
  --rule ../static-extract-go/examples/conformance/http-handlefunc/rule.ser
```

## Engine integration

```bash
-Dcodegraph.parser.process.languages=go
-Dcodegraph.parser.process.go.command="/abs/path/code-graph-parser-go --stdio"
```

stdin: ParseRequest JSON (include `ruleSources` + `externalValues` for endpoints)  
stdout: GraphDelta JSON
