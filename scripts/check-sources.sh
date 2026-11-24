#!/usr/bin/env sh
# Checks that the checked-in sources agree with what the compiler makes of
# them:
#
#   scripts/check-sources.sh <kizu>
#
# - every Kizu source is in the form `kizu fmt` writes, so the formatter and
#   the tree never disagree about the canonical form. examples/negative is
#   left out: some of it is wrong on purpose in ways the formatter is not
#   meant to read;
# - the generated blocks of spec/Tls.lean and the generated Quint modules are
#   what std::tls does now, and the generators pass their own tests. Neither
#   Lean nor Quint is needed: a change the proofs would have to answer for
#   shows as a stale block, and `just spec-table` brings it up to date;
# - the std structs the C runtime reads field by field (std::fs::DirEntry,
#   std::fs::Metadata, std::string::String) have the fields of their C twins
#   in compiler/src/internal/native/runtime_source.kizu, so a change to one
#   side alone fails here (scripts/layout-twins).
#
# scripts/bootstrap.sh runs it with stage1, from the repository root.
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <kizu>" >&2
  exit 2
fi
kizu="$1"

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

default_jobs() {
  n=''
  if command -v nproc >/dev/null 2>&1; then
    n="$(nproc 2>/dev/null || true)"
  elif command -v getconf >/dev/null 2>&1; then
    n="$(getconf _NPROCESSORS_ONLN 2>/dev/null || true)"
  elif command -v sysctl >/dev/null 2>&1; then
    n="$(sysctl -n hw.ncpu 2>/dev/null || true)"
  fi
  case "$n" in
    '' | *[!0-9]* | 0) n=4 ;;
  esac
  if [ "$n" -gt 8 ]; then n=8; fi
  echo "$n"
}

fmt_jobs="${KIZU_FMT_JOBS:-$(default_jobs)}"
case "$fmt_jobs" in
  '' | *[!0-9]* | 0)
    echo "error: KIZU_FMT_JOBS must be a positive integer (got '$fmt_jobs')" >&2
    exit 2
    ;;
esac

unformatted="$(
  find lib/kizu/std/src examples tests/behavior/src tests/unicode/src compiler/src compiler/tests/harness/src \
    -path examples/negative -prune -o -name '*.kizu' -print0 |
    xargs -0 -P "$fmt_jobs" -n 32 sh -c '
      kizu="$1"
      shift
      for file in "$@"; do
        if ! "$kizu" fmt "$file" | cmp -s - "$file"; then
          printf "%s\n" "$file"
        fi
      done
    ' _ "$kizu" |
    sort
)"
if [ -n "$unformatted" ]; then
  echo "these files differ from \`kizu fmt\`; run \`kizu fmt --write\` on them:" >&2
  echo "$unformatted" >&2
  exit 1
fi
echo "fmt: ok"

tables="$(mktemp)"
trap 'rm -f "$tables"' EXIT
"$kizu" run spec/tls_machine.kizu > "$tables"
"$kizu" run scripts/machine-lean/main.kizu -- -check spec/Tls.lean client server < "$tables"
"$kizu" run scripts/machine-tla/main.kizu -- -check spec/tls_client.tla spec/tls_server.tla < "$tables"
"$kizu" test scripts/machine-lean/main.kizu
"$kizu" test scripts/machine-tla/main.kizu
echo "spec tables: ok"

"$kizu" run scripts/layout-twins/main.kizu
"$kizu" test scripts/layout-twins/main.kizu
echo "layout twins: ok"
