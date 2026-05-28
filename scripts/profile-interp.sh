#!/usr/bin/env sh
# Fast interpreter hot-path profiling loop for perf work (issue #976).
#
# BenchmarkInterpHotPath is a seconds-scale proxy for the ~350s selfhost
# backend gate: it exercises the same dominant interpreter functions (eval
# dispatch, binary/logical operators, identifier resolution, calls, loops),
# so a change that speeds those up shows here too. Use it to iterate quickly,
# then confirm real wall-time deltas with the ground-truth gate (see bottom).
#
# Usage:
#   scripts/profile-interp.sh                 # profile, then show top hotspots
#   scripts/profile-interp.sh list Env.Get    # source-annotated listing for a symbol
#   BENCHTIME=10x scripts/profile-interp.sh    # longer run for steadier samples
#
# Quantify a change (A/B) with benchstat:
#   scripts/profile-interp.sh bench >before.txt   # before your change
#   scripts/profile-interp.sh bench >after.txt    # after your change
#   go run golang.org/x/perf/cmd/benchstat@latest before.txt after.txt
set -eu

bench='^BenchmarkInterpHotPath$'
prof="${PROFILE_OUT:-/tmp/kizu-interp.prof}"
benchtime="${BENCHTIME:-5x}"
mode="${1:-top}"

if [ "$mode" = "bench" ]; then
	# Plain benchstat-friendly output; no profiling overhead.
	exec go test ./internal/interp -run '^$' -bench "$bench" \
		-benchtime "$benchtime" -count="${BENCHCOUNT:-6}"
fi

go test ./internal/interp -run '^$' -bench "$bench" \
	-benchtime "$benchtime" -cpuprofile "$prof" -count=1

if [ "$mode" = "list" ]; then
	symbol="${2:?usage: scripts/profile-interp.sh list <symbol>}"
	go tool pprof -list "$symbol" "$prof"
else
	go tool pprof -top -nodecount="${NODES:-20}" "$prof"
fi

# Ground truth (slow, ~350s) -- confirm wall-time deltas with the real gate:
#   KIZU_RUN_SELFHOST_GATES=1 go test ./cmd/kizu \
#     -run '^TestSelfhostBackendArtifactGate$' \
#     -cpuprofile /tmp/kizu-gate.prof -timeout 20m -count=1 -v
