# Contributing to Kizu

Thank you for your interest in Kizu. Bug reports and design discussion are
welcome in [issues](https://github.com/kizu-lang/kizu/issues). Please open an
issue before you write a large change, so that your work is not wasted.

## What a pull request can change

| Area | Paths | How it is handled |
| --- | --- | --- |
| Open | `README.md`, `README.ja.md`, `docs/`, `examples/`, `tests/behavior/` | Open a pull request |
| Agreed first | `compiler/`, `lib/kizu/std/` | Needs an open issue labeled `accepted`. Link it with `Closes #N` |
| Maintainers only | Everything else, including `SPEC.md`, `docs/principles.md`, `docs/adr/`, `.github/`, `scripts/`, `justfile`, `flake.nix`, and every `AGENTS.md`, `CLAUDE.md`, `.claude/` and `.envrc` | Please open an issue instead |

Binary files are not accepted from outside the project, and neither is a diff
too large to review. If a change needs one, describe it in an issue and a
maintainer will produce the file.

A maintainer adds the `accepted` label once an issue is agreed. A pull request
without an accepted issue may be closed without review, however good the code
is. Changes to the language itself start as a design discussion in an issue.

## How a pull request is checked

An automated check labels each pull request from outside the project and fails
when it goes outside the table above. A failing check is a request to change
the scope of the pull request. It is not a judgment of the work.

CI runs after a maintainer approves it, on every push. Maintainers review the
exact commit that is merged, so a push after review starts the review again.

## Before you open a pull request

Build and test the way CI does, inside `nix develop`:

```sh
just selfhost
pre-commit run --all-files
```

- Keep each pull request to one purpose.
- Add a test that fails without the change. See [AGENTS.md](AGENTS.md) for the
  rules on tests and comments.
- Say what you changed, why, and how you verified it.
- Say so if an AI tool wrote part of the change. You are responsible for all of
  it either way.

## License

By contributing, you agree that your contribution is licensed under the
[MIT License](LICENSE).
