#!/usr/bin/env sh
set -eu

run() {
  name="$1"
  shift

  start="$(date +%s)"
  "$@"
  end="$(date +%s)"

  elapsed="$((end - start))"
  printf '%s\t%ss\n' "$name" "$elapsed"
}

run "go test" go test ./...
run "parse hello" go run ./cmd/kizu parse examples/hello.kizu
run "ir hello" go run ./cmd/kizu ir examples/hello.kizu
run "llvm hello" go run ./cmd/kizu build --emit-llvm examples/hello.kizu
run "llvm hello no-op" go run ./cmd/kizu build --emit-llvm examples/hello.kizu
run "why hello" go run ./cmd/kizu why-rebuild examples/hello.kizu
run "cache status" go run ./cmd/kizu cache status
run "run hello" go run ./cmd/kizu run examples/hello.kizu
run "pre-commit" pre-commit run --all-files
