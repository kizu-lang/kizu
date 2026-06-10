# CLAUDE.md

Follow `AGENTS.md` as the primary repository guidance.

## Selfhost Gate Policy

- Do not run interpreted selfhost gates as a routine validation loop.
- Prefer `just selfhost-fast-gate` when a passing `target/selfhost/stage2/selfhost`
  artifact already exists.
- Use `just selfhost-production-from-scratch` only at selfhost source checkpoints.
- Use `just selfhost-native-source-gate` or the hosted stage2 parity gates for
  run/test executable work before reaching for interpreter-only internals.
- `TestSelfhostRunTapeLoweringGate`, `TestSelfhostRunRenderGate`, and similar
  `interp.New(...).RunEntry(...)` gates execute selfhost compiler code through
  the Go interpreter. They can take many minutes and are debug-only.
- `TestSelfhostFormatDriverFactsGate` and `TestSelfhostFormatDriverLoweringGate`
  are also interpreter-backed focused gates; run them only with
  `KIZU_RUN_SELFHOST_GATES=1` when pinning a format-driver blocker.
- The run tape/render interpreter gates intentionally have no `just` recipes.
  If one is truly needed to pin a measured blocker, run the raw `go test`
  command with a clear time budget and write full output to a log file.
- Do not pipe long selfhost gates through `tail`; it hides failures and makes
  stalled interpreter runs look like silent progress.
