#!/usr/bin/env sh
set -eu

status=0

check_package_comment() {
  dir="$1"
  pkg="$2"
  found=0

  for file in "$dir"/*.go; do
    [ -e "$file" ] || continue
    if [ "$pkg" = "main" ]; then
      if awk '/^\/\/ Command / { found=1 } /^package / { exit(found ? 0 : 1) }' "$file"; then
        found=1
        break
      fi
    else
      if awk '/^\/\/ Package / { found=1 } /^package / { exit(found ? 0 : 1) }' "$file"; then
        found=1
        break
      fi
    fi
  done

  if [ "$found" -ne 1 ]; then
    if [ "$pkg" = "main" ]; then
      printf '%s: missing command comment\n' "$dir" >&2
    else
      printf '%s: missing package comment\n' "$dir" >&2
    fi
    status=1
  fi
}

check_function_comments() {
  file="$1"

  awk '
    /^func[[:space:]]/ {
      if (prev !~ /^\/\/[[:space:]]/) {
        printf "%s:%d: missing function comment\n", FILENAME, FNR
        status = 1
      }
    }
    { prev = $0 }
    END { exit status }
  ' "$file" || status=1
}

files="$(find . -path './.direnv' -prune -o -path './.git' -prune -o -name '*.go' -print)"

for file in $files; do
  check_function_comments "$file"
done

dirs="$(printf '%s\n' "$files" | xargs -n1 dirname | sort -u)"

for dir in $dirs; do
  first_go="$(find "$dir" -maxdepth 1 -name '*.go' | sort | head -n 1)"
  pkg="$(awk '/^package / { print $2; exit }' "$first_go")"
  check_package_comment "$dir" "$pkg"
done

exit "$status"
