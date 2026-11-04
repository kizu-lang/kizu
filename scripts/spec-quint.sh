#!/usr/bin/env bash
# Searches random runs of the generated Quint machines for a broken property, and
# fails if the runs never reach the state the properties are about. Quint comes
# through npx at a fixed version and is not a dependency of the repository. This is
# a search and not a proof: spec-verify.sh is exhaustive but needs Java.
set -euo pipefail
cd "$(dirname "$0")/../spec"

quint=(npx -y @informalsystems/quint@0.33.0)
search=(--backend typescript --seed 0x1 --max-samples 1000 --max-steps 40)

"${quint[@]}" typecheck tls_properties.qnt > /dev/null

for check in \
    tls_client_properties:connectedOnlyByFinished \
    tls_client_properties:closedStaysClosed \
    tls_client_properties:dataNeedsConnection \
    tls_server_properties:connectedOnlyByFinished \
    tls_server_properties:closedStaysClosed; do
    main=${check%%:*}
    invariant=${check##*:}
    if ! output=$("${quint[@]}" run tls_properties.qnt --main="$main" "${search[@]}" \
        --invariant="$invariant" --witnesses=reachedConnected 2>&1); then
        echo "$output"
        echo "spec-quint: $check is broken" >&2
        exit 1
    fi
    if echo "$output" | grep -q 'reachedConnected was witnessed in 0 trace'; then
        echo "$output"
        echo "spec-quint: $check: no run reached the connected state" >&2
        exit 1
    fi
    echo "$check: $(echo "$output" | grep 'reachedConnected was witnessed')"
done
