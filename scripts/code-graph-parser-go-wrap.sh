#!/usr/bin/env bash
export PATH="${HOME}/.local/go/bin:/usr/local/go/bin:${PATH}"
DIR="$(cd "$(dirname "$0")" && pwd)"
exec "$DIR/code-graph-parser-go" "$@"
