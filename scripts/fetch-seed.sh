#!/usr/bin/env sh
# Downloads the seed compiler pinned in scripts/seed.txt, checks its sha256,
# and extracts it under target/seed/. Prints the path of the seed binary.
#
#   scripts/fetch-seed.sh [platform]
#
# The platform defaults to the host's (darwin-arm64, linux-amd64,
# linux-arm64, windows-amd64). An archive whose digest differs from the pin is
# deleted and the script fails: nothing unverified is extracted or run.
set -eu

root="$(cd "$(dirname "$0")/.." && pwd)"
pin="$root/scripts/seed.txt"

host_platform() {
  os="$(uname -s)"
  arch="$(uname -m)"
  case "$os" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    MINGW* | MSYS* | CYGWIN*) os=windows ;;
    *) echo "error: no seed for OS $os" >&2; exit 1 ;;
  esac
  case "$arch" in
    arm64 | aarch64) arch=arm64 ;;
    x86_64 | amd64) arch=amd64 ;;
    *) echo "error: no seed for architecture $arch" >&2; exit 1 ;;
  esac
  echo "$os-$arch"
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

platform="${1:-$(host_platform)}"
version="$(awk '$1 == "version" { print $2 }' "$pin")"
want="$(awk -v p="$platform" '$1 == "sha256" && $2 == p { print $3 }' "$pin")"
if [ -z "$version" ]; then
  echo "error: $pin names no version" >&2
  exit 1
fi
if [ -z "$want" ]; then
  echo "error: $pin pins no $platform archive" >&2
  exit 1
fi

name="kizu-$version-$platform"
case "$platform" in
  windows-*) archive="$name.zip"; exe=kizu.exe ;;
  *) archive="$name.tar.gz"; exe=kizu ;;
esac

dir="$root/target/seed"
mkdir -p "$dir"
file="$dir/$archive"
# Extracted fresh every time, from the archive checked below, so a binary left
# by an earlier run is neither what runs nor left behind when the check fails.
rm -rf "${dir:?}/$name"
if [ ! -f "$file" ] || [ "$(sha256_of "$file")" != "$want" ]; then
  rm -f "$file"
  url="https://github.com/kizu-lang/kizu/releases/download/$version/$archive"
  echo "fetching $url" >&2
  curl -fsSL --retry 3 -o "$file.part" "$url"
  mv "$file.part" "$file"
fi
got="$(sha256_of "$file")"
if [ "$got" != "$want" ]; then
  rm -f "$file"
  echo "error: $archive has sha256 $got, $pin pins $want" >&2
  exit 1
fi

case "$archive" in
  *.zip) unzip -q "$file" -d "$dir" ;;
  *) tar -xzf "$file" -C "$dir" ;;
esac
seed="$dir/$name/bin/$exe"
if [ ! -f "$seed" ]; then
  echo "error: $archive has no bin/$exe" >&2
  exit 1
fi
echo "$seed"
