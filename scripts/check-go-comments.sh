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

# .claude holds agent worktree checkouts -- copies of this repository that git does not
# track here and that are checked by their own working tree, not by this one.
found="$(find . \
  -path './.direnv' -prune -o \
  -path './.git' -prune -o \
  -path './.claude' -prune -o \
  -name '*.go' -print)"

# What git ignores is not this repository's source: a local scratch tree that
# happens to hold Go files is checked by whoever put it there, if at all. The
# question is asked of git rather than answered by a list here, so a new one
# does not have to be added to that list to stop failing this hook.
files=""
for file in $found; do
  if git check-ignore -q "$file" 2>/dev/null; then
    continue
  fi
  files="$files $file"
done

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
