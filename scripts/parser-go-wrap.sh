#!/usr/bin/env bash
# Go itself may be installed under ~/.local/go, while `go install` places
# commands such as gopls under the default GOPATH bin directory (~/go/bin).
export PATH="${HOME}/.local/go/bin:${HOME}/go/bin:/usr/local/go/bin:${PATH}"
DIR="$(cd "$(dirname "$0")" && pwd)"
exec "$DIR/parser-go" "$@"
