#!/usr/bin/env sh
set -eu

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

export KIZU_CACHE_DIR="$tmp/cache"
cp examples/hello.kizu "$tmp/hello.kizu"

# The compiler is built once. Measuring through `go run` would put Go's own
# build machinery in every number, which is larger than the cache difference
# this script exists to show.
kizu="$tmp/kizu"
go build -o "$kizu" ./cmd/kizu

# A cache hit is faster than `time` can resolve, so a warm measurement is the
# average of repeats rather than one reading that would print as 0.00s. A cold
# measurement is a single run by definition, and averaging it with the warm runs
# that follow would hide exactly what it is there to show.
warm=10
cold=1
cat > "$tmp/repeat.sh" <<'EOF'
#!/usr/bin/env sh
set -eu
n="$1"
shift
i=0
while [ "$i" -lt "$n" ]; do
  "$@" >/dev/null
  i=$((i + 1))
done
EOF
chmod +x "$tmp/repeat.sh"

run() {
  name="$1"
  n="$2"
  shift 2

  out="$tmp/time.txt"
  if /usr/bin/time -p "$tmp/repeat.sh" "$n" "$@" 2>"$out"; then
    awk -v name="$name" -v n="$n" \
      '/^real / { printf "%s\t%.0fms\n", name, $2 * 1000 / n }' "$out"
  else
    cat "$out" >&2
    return 1
  fi
}

# The compiler binary is new, and a system that checks a binary the first time
# it runs would charge that check to whichever measurement came first.
"$kizu" cache status >/dev/null

# The edit changes what the program prints, so it reaches every cache rather
# than stopping at the first one a whitespace change would miss.
edit() {
  sed 's/hello, kizu/'"$1"'/' examples/hello.kizu > "$tmp/hello.kizu"
}

edit "hello, kizu"
run "cold llvm" "$cold" "$kizu" build --emit-llvm "$tmp/hello.kizu"
run "warm llvm" "$warm" "$kizu" build --emit-llvm "$tmp/hello.kizu"
run "cold wasm" "$cold" "$kizu" build --target wasm32-wasi "$tmp/hello.kizu"
run "warm wasm" "$warm" "$kizu" build --target wasm32-wasi "$tmp/hello.kizu"
run "cold run" "$cold" "$kizu" run "$tmp/hello.kizu"
run "warm run" "$warm" "$kizu" run "$tmp/hello.kizu"
edit "edited, kizu"
run "single-file edit llvm" "$cold" "$kizu" build --emit-llvm "$tmp/hello.kizu"
run "single-file edit run" "$cold" "$kizu" run "$tmp/hello.kizu"
run "cache status" "$warm" "$kizu" cache status
printf 'cache size\t%s\n' "$(du -sh "$KIZU_CACHE_DIR" | awk '{ print $1 }')"
