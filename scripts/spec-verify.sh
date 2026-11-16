#!/usr/bin/env bash
# Checks every reachable state of the generated TLA+ machines with TLC. It takes
# tlc from PATH, or else from nixpkgs' tlaplus, which brings its own JDK; without
# either it fails, since skipping would read as a pass. A machine that can never
# connect would satisfy the properties about connecting, so notConnected has to
# be broken.
set -euo pipefail
cd "$(dirname "$0")/../spec"

if ! command -v tlc > /dev/null; then
    if ! command -v nix > /dev/null || ! tlaplus=$(nix build --no-link --print-out-paths nixpkgs#tlaplus); then
        echo "spec-verify: needs tlc or nix" >&2
        exit 1
    fi
    export PATH="$tlaplus/bin:$PATH"
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

verify() {
    printf 'SPECIFICATION Spec\nINVARIANT %s\n' "$2" > "$work/$1.cfg"
    tlc -config "$work/$1.cfg" -metadir "$work/states" -workers 1 "$1.tla" 2>&1
}

for check in \
    tls_client_properties:TypeOK \
    tls_client_properties:connectedOnlyByFinished \
    tls_client_properties:closedStaysClosed \
    tls_client_properties:dataNeedsConnection \
    tls_server_properties:TypeOK \
    tls_server_properties:connectedOnlyByFinished \
    tls_server_properties:closedStaysClosed; do
    main=${check%%:*}
    invariant=${check##*:}
    if ! output=$(verify "$main" "$invariant"); then
        echo "$output"
        echo "spec-verify: $check is broken" >&2
        exit 1
    fi
    echo "$check: $(echo "$output" | grep 'distinct states found')"
done

for main in tls_client_properties tls_server_properties; do
    if output=$(verify "$main" notConnected); then
        echo "$output"
        echo "spec-verify: $main: the connected state is unreachable" >&2
        exit 1
    fi
    if ! echo "$output" | grep -q 'Invariant notConnected is violated'; then
        echo "$output"
        echo "spec-verify: $main: TLC did not finish" >&2
        exit 1
    fi
done
