#!/bin/sh
set -eu
cd "$(dirname "$0")"
command -v go >/dev/null 2>&1 || { echo 'Install Go 1.25 or newer first.' >&2; exit 1; }
go mod download
if ! command -v wails >/dev/null 2>&1; then
  go install github.com/wailsapp/wails/v2/cmd/wails@v2.16.0
  export PATH="$(go env GOPATH)/bin:$PATH"
fi
case "${1:-build}" in
  test) go test ./internal/...; go vet ./internal/... ;;
  dev) wails dev ;;
  universal) wails build -platform darwin/universal ;;
  *) if [ "$(uname -s)" = Linux ] && pkg-config --exists webkit2gtk-4.1; then
       wails build -tags webkit2_41
     else
       wails build
     fi ;;
esac
