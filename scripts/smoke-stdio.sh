#!/usr/bin/env bash
set -euo pipefail
export PATH="${HOME}/.local/go/bin:${PATH}"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"
go build -o bin/code-graph-parser-go ./cmd/code-graph-parser-go
EX="$(cd ../static-extract-go/examples/conformance/http-handlefunc && pwd)"
SER=$(python3 -c "import json; print(json.dumps(open('$EX/rule.ser').read()))")
OUT=$(printf '%s' "{\"projectName\":\"demo\",\"language\":\"go\",\"projectRoot\":\"$EX\",\"ruleSources\":[$SER]}" \
  | ./bin/code-graph-parser-go --stdio)
echo "$OUT" | python3 -c "import sys,json; d=json.load(sys.stdin); assert len(d['endpoints'])>=2, d; print('OK endpoints', len(d['endpoints']), 'funcs', len(d['functions']))"
