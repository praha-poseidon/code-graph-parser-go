#!/usr/bin/env bash
set -euo pipefail
export PATH="${HOME}/.local/go/bin:${PATH}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
go test ./...
go build -o bin/parser-go ./cmd/parser-go
WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT
mkdir -p "$WORKDIR/proj"
cat > "$WORKDIR/proj/go.mod" << 'EOF'
module example.com/verify
go 1.22
EOF
cat > "$WORKDIR/proj/main.go" << 'EOF'
package main
import "net/http"
func main() {
  http.HandleFunc("/api/users", users)
  http.HandleFunc("/api/health", health)
  http.HandleFunc("/api/orders", orders)
  _ = http.ListenAndServe(":8080", nil)
}
func users(w http.ResponseWriter, r *http.Request)  { helper() }
func health(w http.ResponseWriter, r *http.Request) {}
func orders(w http.ResponseWriter, r *http.Request) {}
func helper() {}
EOF
cat > "$WORKDIR/rule.ser" << 'EOF'
rule "net/http HandleFunc inbound"
endpoint HTTP inbound
find call HandleFunc
let path =
  from argument 0 take value
build {
  httpMethod: "ANY"
  path: path
}
EOF
SER=$(python3 -c "import json; print(json.dumps(open('$WORKDIR/rule.ser').read()))")
run() {
  printf '%s' "$1" | ./bin/parser-go --stdio
}
echo "[1] full + 3 endpoints + e2f"
OUT=$(run "{\"projectName\":\"v\",\"language\":\"go\",\"projectRoot\":\"$WORKDIR/proj\",\"ruleSources\":[$SER]}")
python3 -c "
import json,sys
from collections import Counter
d=json.loads(sys.argv[1])
assert len(d['endpoints'])==3
assert sum(1 for r in d['relationships'] if r['relationshipType']=='ENDPOINT_TO_FUNCTION')==3
print(' OK', dict(Counter(r['relationshipType'] for r in d['relationships'])))
" "$OUT"
echo "[2] after remove orders"
cat > "$WORKDIR/proj/main.go" << 'EOF'
package main
import "net/http"
func main() {
  http.HandleFunc("/api/users", users)
  http.HandleFunc("/api/health", health)
  _ = http.ListenAndServe(":8080", nil)
}
func users(w http.ResponseWriter, r *http.Request)  { helper() }
func health(w http.ResponseWriter, r *http.Request) {}
func helper() {}
EOF
OUT=$(run "{\"projectName\":\"v\",\"language\":\"go\",\"projectRoot\":\"$WORKDIR/proj\",\"sourceFiles\":[\"$WORKDIR/proj/main.go\"],\"changeType\":\"SOURCE_MODIFIED\",\"ruleSources\":[$SER]}")
python3 -c "
import json,sys
d=json.loads(sys.argv[1])
paths=set(e['path'] for e in d['endpoints'])
names=set(f['name'] for f in d['functions'] if not f.get('isPlaceholder'))
assert paths=={'/api/users','/api/health'}, paths
assert 'orders' not in names
assert len(d.get('deletedNodeIds') or [])==0
fns={f['id']:f for f in d['functions']}
for r in d['relationships']:
  if r['relationshipType']!='ENDPOINT_TO_FUNCTION': continue
  fn=fns[r['toNodeId']]
  assert not fn.get('isPlaceholder')
  assert fn['name'] in ('users','health')
pairs={(fns[r['fromNodeId']]['name'], fns[r['toNodeId']]['name']) for r in d['relationships'] if r['relationshipType']=='CALLS' and r['fromNodeId'] in fns and r['toNodeId'] in fns}
assert ('users','helper') in pairs, pairs
print(' OK incremental endpoints+calls+no-delete-list')
" "$OUT"
echo "ALL VERIFY PASS"
