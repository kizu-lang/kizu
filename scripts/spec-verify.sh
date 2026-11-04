#!/usr/bin/env bash
# Checks every reachable state of the generated Quint machines with TLC, through
# `quint verify --backend=tlc`. Unlike spec-quint.sh this is exhaustive, and it needs
# Java: the one on PATH, or else the JDK from nixpkgs. Without either it fails, since
# skipping would read as a pass. A machine that can never connect would satisfy the
# properties about connecting, so notConnected has to be broken.
set -euo pipefail
cd "$(dirname "$0")/../spec"

if ! java -version > /dev/null 2>&1; then
    if ! command -v nix > /dev/null || ! jdk=$(nix build --no-link --print-out-paths nixpkgs#jdk21_headless); then
        echo "spec-verify: needs Java or nix" >&2
        exit 1
    fi
    export PATH="$jdk/bin:$PATH" JAVA_HOME="$jdk"
fi

trap 'rm -rf _apalache-out' EXIT
quint=(npx -y @informalsystems/quint@0.33.0)

verify() {
    "${quint[@]}" verify --backend=tlc --main="$1" --invariant="$2" tls_properties.qnt 2>&1
}

for check in \
    tls_client_properties:connectedOnlyByFinished \
    tls_client_properties:closedStaysClosed \
    tls_client_properties:dataNeedsConnection \
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
    if ! echo "$output" | grep -q '\[violation\]'; then
        echo "$output"
        echo "spec-verify: $main: TLC did not finish" >&2
        exit 1
    fi
done
