# ADR-0076: public command owner transition

## Status

Proposed.

## Context

Issue #1075 asks how to promote the stage2 selfhost artifact from a separate
compiler artifact into the owner of the public `kizu` command. This ADR is a
draft for human review, not the final switch decision.

The current public command boundary is still mixed:

- `cmd/kizu/main.go` owns public dispatch.
- `parse`, `check`, and `fmt` enter selfhost code through the Go interpreter.
- `run` is unconditionally selfhost-owned with no Go interpreter fallback;
  `test` still has the rollback-friendly `KIZU_SELFHOST_TEST` switch before its
  default Go interpreter path.
- `target/selfhost/stage2/selfhost` owns the #458 production artifact path and
  bounded hosted parity rows, but it is not yet the public command owner.
- Deferral diagnostics and cache logic for the selfhost path have landed.
- Production evidence already uses explicit no-fallback markers such as
  `go.production none`, `go.cmd-kizu-fallback none`, and `go.fallback none`.

The transition must preserve the required public `run`, `parse`, and `check`
semantics for supported shapes. Unsupported or deferred commands must fail
visibly and must not re-enter Go as a hidden compatibility path.

## Decision 1: Public command transition mechanism

### Decision drivers

- Make no-hidden-fallback easy to audit.
- Keep rollback explicit and small.
- Keep production reports aligned with the existing no-Go markers.
- Avoid a bootstrap chicken-and-egg where the new public command cannot be
  produced or installed without already having the new public command.
- Preserve a path for commands that are intentionally deferred.

### Options

1. Artifact replacement: install the selfhost artifact itself as `kizu`.
2. Generated launcher: install a small generated public launcher that execs the
   selfhost artifact for selfhost-owned commands and returns explicit deferral
   diagnostics for deferred commands.
3. Go wrapper: keep Go `cmd/kizu` as the public process and delegate selected
   commands to the selfhost artifact.

### Trade-offs

Artifact replacement gives the cleanest ownership story. The public binary is
the selfhost compiler, so there is no wrapper dispatch surface where Go fallback
can hide. It also makes production reports straightforward: the public command
and the stage artifact are the same executable. The cost is rollback and
packaging complexity. Any deferred command must already have a selfhost
diagnostic path, and the build/install process must carefully document the
stage0 Go bootstrap boundary.

The generated launcher is a smaller transition step. The launcher can be audited
to contain only command classification, artifact path resolution, `exec`, and
explicit deferral diagnostics. It can keep rollback as a launcher/artifact
replacement while still making every selfhost-owned command run in the stage2
artifact with no Go compiler phase fallback. The cost is that there are two
public artifacts to package and report, and the launcher command table must not
become a second hand-maintained compiler dispatch.

The Go wrapper is easiest to land mechanically and easiest to revert, but it is
the weakest ownership boundary. Public `kizu` would still be Go-owned, hidden
fallback is harder to rule out, and production report fields would need to prove
both wrapper behavior and artifact behavior. It also conflicts with the #1075
goal that Go entry points remain explicit bootstrap, oracle, test, or recovery
commands.

### Recommendation

Use the generated launcher as the first public-command transition mechanism.
The launcher must be generated or checked from a small ownership manifest with
only two command states:

- selfhost-owned: exec the selected stage2/stage3 artifact with unchanged
  command arguments.
- deferred: print a stable diagnostic, include the linked issue when known, and
  exit without invoking Go.

The launcher must not import Go compiler packages, run Go parser/checker/backend
code, or interpret `KIZU_SELFHOST_*` as fallback switches. `run`, `parse`, and
`check` should be selfhost-owned before the public switch is claimed. `test` may
remain gated until its selected surface is ready, but a public launcher default
must be either selfhost-owned or explicitly deferred, never Go fallback.

Keep artifact replacement as the terminal shape after deferred public commands
are either selfhost-owned or intentionally removed/split out. Do not choose a Go
wrapper for #1075 except as an explicitly named recovery tool outside the public
production command.

### Consequences

- The production report for the public switch should identify the launcher and
  artifact path, then record no-Go markers for each claimed command.
- Rollback is a revert or replacement of the explicit launcher/artifact switch,
  not environment-sensitive fallback to Go compiler phases.
- Unsupported command behavior belongs in selfhost diagnostics or launcher
  deferral diagnostics.
- The command ownership manifest becomes review-critical for future command
  promotions.

## Decision 2: Cache root policy

### Decision drivers

- Keep Go cache ownership separate from selfhost artifact ownership.
- Preserve deterministic bootstrap and production reports.
- Avoid unbounded cache growth or hidden writes outside reported roots.
- Leave room for a later user-cache migration without blocking #1075.

### Options

1. Converge immediately on the existing Go cache root:
   `KIZU_CACHE_DIR` or the OS user cache directory.
2. Use the existing selfhost root, `target/selfhost/cache`, for all
   selfhost-owned public commands.
3. Keep a dual-root transition: selfhost-owned public commands use
   `target/selfhost/cache`, while Go cache roots remain only for explicitly
   deferred Go tools or bootstrap/oracle commands.

### Trade-offs

Using the Go cache root preserves existing user expectations for `KIZU_CACHE_DIR`
and `kizu cache`, but it mixes ownership at the exact point where #1075 is
trying to remove Go from the production command. It also requires the selfhost
artifact to implement OS user-cache discovery, env-var override behavior,
status, prune, and key migration before the public switch.

Using only `target/selfhost/cache` matches the selfhost cache work that has
landed and keeps bootstrap reports deterministic. It is easy to audit because
the artifact writes under the same reported target tree used by selfhost gates.
The cost is that it does not preserve the Go cache command behavior for users
and may duplicate entries while Go-owned build/cache commands still exist.

The dual-root transition makes coexistence explicit. Selfhost-owned public
commands never read or write the Go build cache. Go cache roots remain available
only through explicit Go-owned tools, bootstrap, oracle, or recovery commands
that are not claimed as selfhost production paths. The cost is documenting the
temporary split and adding a later migration issue when selfhost owns `cache`
and `why-rebuild`.

### Recommendation

Use the dual-root transition, with `target/selfhost/cache` as the only cache root
for selfhost-owned public commands in #1075. The generated launcher must not
translate `KIZU_CACHE_DIR` into a selfhost cache path. If the selfhost artifact
later accepts a cache-root environment variable, that behavior must be
selfhost-implemented, reported, and covered by a cache gate.

Coexistence rules:

- `run`, `parse`, `check`, `fmt`, and `test` selfhost paths may write only to
  reported selfhost target/cache roots.
- Go `KIZU_CACHE_DIR` and OS user-cache roots remain Go-owned until a separate
  selfhost cache issue implements status, prune, key inputs, and migration.
- Public `cache` and `why-rebuild` must be selfhost-owned before being claimed
  by the public artifact. Until then they are deferred or moved to an explicit
  recovery/tooling command outside the production `kizu` surface.
- Cache reports must name the root, key inputs, artifact sizes, and prune/status
  behavior for every public command that writes durable entries.

### Consequences

- #1075 does not need to solve user-cache migration before promoting the
  frontend command owner.
- Users may temporarily see separate Go and selfhost cache trees.
- A later cache-command issue must decide whether selfhost stays project-local
  or adopts `KIZU_CACHE_DIR`/OS user-cache semantics.
- Production gates can reject accidental Go-cache access in selfhost-owned
  commands.

## Decision 3: `init` and `import-c-header`

### Decision drivers

- Keep the v0 public compiler command focused on selfhost-owned compiler paths.
- Avoid adding a C parser or scaffolding subsystem as a hidden prerequisite for
  #1075.
- Keep user-visible unsupported behavior stable.
- Avoid keeping Go helper behavior behind the public `kizu` name after the owner
  switch.

### Options

1. Selfhost now: implement each command in the selfhost artifact before the
   public switch.
2. Explicit deferral: keep the command name in the public surface but return a
   stable unsupported/deferred diagnostic with a linked issue.
3. Separate tool: move the command to an explicitly named non-production helper
   outside the selfhost-owned public `kizu` command.

### Trade-offs

Selfhosting `init` is plausible because it mostly needs path, filesystem, and
string capabilities that already exist in some form. It still needs package-name
normalization, overwrite refusal, atomic writes, and parity gates, so it should
not block #1075 unless a child issue chooses to make it part of the switch.

Selfhosting `import-c-header` is not a good #1075 dependency. It would require a
C declaration parser/importer surface unrelated to the required `run`, `parse`,
and `check` compiler semantics. Keeping the Go importer behind the public
command would violate the ownership goal.

Explicit deferral is safest for the public switch, but it is a user-visible
regression for commands documented today. Separate tools preserve access to
non-core Go helpers while making their ownership visible.

### Recommendation

Do not require `init` or `import-c-header` for the first #1075 public owner
switch.

For `init`, prefer explicit deferral in the generated launcher unless a bounded
child issue implements it in selfhost first. A future selfhost `init` command is
reasonable if it uses selfhost `std::fs`/`std::path`, refuses overwrites, writes
the same minimal package shape, and has command-specific gates.

For `import-c-header`, prefer a separate explicitly named tool or permanent
deferral from the selfhost-owned `kizu` surface. It should not be routed to Go
from public `kizu` after the owner switch. If C import becomes part of the
selfhost compiler later, it needs its own ADR or issue covering the C subset,
diagnostics, and cache inputs.

### Consequences

- The first public switch can focus on `run`, `parse`, and `check` plus any
  command slices that already have selfhost gates.
- Existing users of `init` or `import-c-header` need a documented transition
  path before a release uses the launcher.
- Reviewers can audit that deferred commands fail visibly instead of crossing
  back into Go.
- Future command promotions must add command-specific evidence before changing
  the ownership manifest.
