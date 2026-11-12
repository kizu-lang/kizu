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

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

# The compiler measured is compiler/ built by the pinned seed. It has no
# library tree next to it, so it reads the checkout's.
run "bootstrap" scripts/bootstrap.sh
kizu="$root/target/seed/stage1/bin/kizu"
export KIZU_LIB_DIR="$root/lib/kizu"

run "parse hello" "$kizu" parse examples/hello.kizu
run "check hello" "$kizu" check examples/hello.kizu
run "run hello" "$kizu" run examples/hello.kizu
run "parse user_registry" "$kizu" parse examples/user_registry.kizu
run "check user_registry" "$kizu" check examples/user_registry.kizu
run "run user_registry" "$kizu" run examples/user_registry.kizu
run "ir hello" "$kizu" ir examples/hello.kizu
run "llvm hello" "$kizu" build --emit-llvm examples/hello.kizu
run "wasm hello" "$kizu" build --target wasm32-wasi examples/hello.kizu
run "llvm hello no-op" "$kizu" build --emit-llvm examples/hello.kizu
run "wasm hello no-op" "$kizu" build --target wasm32-wasi examples/hello.kizu
run "cache status" "$kizu" cache status
run "pre-commit" pre-commit run --all-files
