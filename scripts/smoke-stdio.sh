#!/usr/bin/env bash
set -euo pipefail
export PATH="${HOME}/.local/go/bin:${PATH}"
ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT_DIR"
go test ./...
go build -o bin/parser-go ./cmd/parser-go
EX="$(cd ../static-extract-go/examples/conformance/http-handlefunc && pwd)"
SER=$(python3 -c "import json; print(json.dumps(open('$EX/rule.ser').read()))")
OUT=$(printf '%s' "{\"projectName\":\"demo\",\"language\":\"go\",\"projectRoot\":\"$EX\",\"ruleSources\":[$SER]}" \
  | ./bin/parser-go --stdio)
echo "$OUT" | python3 -c '
import sys, json
d=json.load(sys.stdin)
assert d.get("packages"), d
assert d.get("functions"), d
assert d.get("relationships"), d
rels=set(r["relationshipType"] for r in d["relationships"])
assert "PACKAGE_TO_UNIT" in rels, rels
assert "UNIT_TO_FUNCTION" in rels, rels
assert "CALLS" in rels, rels
assert len(d.get("endpoints",[]))>=2
for p in d["packages"]:
  assert p.get("qualifiedName") and p.get("projectFilePath") and p.get("projectName")
for f in d["functions"]:
  assert f.get("qualifiedName") and f.get("projectFilePath")
print("OK packages", len(d["packages"]), "units", len(d["units"]), "funcs", len(d["functions"]),
      "rels", len(d["relationships"]), "eps", len(d["endpoints"]), "types", sorted(rels))
'
