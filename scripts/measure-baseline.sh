#!/usr/bin/env sh
set -eu

run() {
  name="$1"
  shift

  tmp="$(mktemp)"
  if /usr/bin/time -p "$@" >/dev/null 2>"$tmp"; then
    elapsed="$(awk '/^real / { print $2 }' "$tmp")"
    printf '%s\t%ss\n' "$name" "$elapsed"
  else
    cat "$tmp" >&2
    rm -f "$tmp"
    return 1
  fi
  rm -f "$tmp"
}

run "go test" go test ./...
run "parse hello" go run ./cmd/kizu parse examples/hello.kizu
run "check hello" go run ./cmd/kizu check examples/hello.kizu
run "run hello" go run ./cmd/kizu run examples/hello.kizu
run "parse user_registry" go run ./cmd/kizu parse examples/user_registry.kizu
run "check user_registry" go run ./cmd/kizu check examples/user_registry.kizu
run "run user_registry" go run ./cmd/kizu run examples/user_registry.kizu
run "ir hello" go run ./cmd/kizu ir examples/hello.kizu
run "llvm hello" go run ./cmd/kizu build --emit-llvm examples/hello.kizu
run "wasm hello" go run ./cmd/kizu build --target wasm32-wasi examples/hello.kizu
run "llvm hello no-op" go run ./cmd/kizu build --emit-llvm examples/hello.kizu
run "wasm hello no-op" go run ./cmd/kizu build --target wasm32-wasi examples/hello.kizu
run "cache status" go run ./cmd/kizu cache status
run "pre-commit" pre-commit run --all-files
