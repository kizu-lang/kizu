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
#   shows as a stale block, and `just spec-table` brings it up to date.
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

unformatted="$(
  find lib/kizu/std/src examples tests/behavior/src compiler/src compiler/tests/harness/src \
    -path examples/negative -prune -o -name '*.kizu' -print | sort |
    while IFS= read -r file; do
      if ! "$kizu" fmt "$file" | cmp -s - "$file"; then echo "$file"; fi
    done
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
"$kizu" run scripts/machine-quint/main.kizu -- -check spec/tls_client.qnt spec/tls_server.qnt < "$tables"
"$kizu" test scripts/machine-lean/main.kizu
"$kizu" test scripts/machine-quint/main.kizu
echo "spec tables: ok"
