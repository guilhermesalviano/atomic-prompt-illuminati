#!/usr/bin/env bash
set -euo pipefail

PROJECT_NAME="prompter-illuminati"
BINARY_NAME="api"
MAIN_PKG="./cmd/api"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

say()  { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
err()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

cd "$(dirname "$0")"

# --- Check Go toolchain -----------------------------------------------------
command -v go >/dev/null 2>&1 || err "Go is not installed. Get it at https://go.dev/dl/"

GO_MINOR=$(go env GOVERSION | sed 's/go1\.\([0-9]*\).*/\1/')
[ "${GO_MINOR:-0}" -ge 22 ] || err "Go >= 1.22 required (found $(go env GOVERSION))"

# --- Download dependencies --------------------------------------------------
say "Downloading dependencies..."
go mod download

# --- Run tests --------------------------------------------------------------
if go test "$MAIN_PKG" >/dev/null 2>&1; then
  say "Tests passed."
else
  err "Tests failed. Run 'go test ./...' for details."
fi

# --- Build ------------------------------------------------------------------
say "Building ${BINARY_NAME}..."
mkdir -p bin
GOFLAGS="-trimpath" go build -o "bin/${BINARY_NAME}" "$MAIN_PKG"
say "Built bin/${BINARY_NAME}"

# --- Install to PATH --------------------------------------------------------
say "Installing to ${INSTALL_DIR}..."
mkdir -p "$INSTALL_DIR"
install -m 0755 "bin/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"

case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    printf '\n\033[1;33mnote:\033[0m %s is not in your PATH.\nAdd it with:\n  export PATH="%s:$PATH"\n' "$INSTALL_DIR" "$INSTALL_DIR"
    ;;
esac

say "Done. Run '${BINARY_NAME} --help' to get started."
