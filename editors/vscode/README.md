# Kizu for Visual Studio Code

This extension registers `.kizu` files with syntax highlighting and snippets,
and starts `kizu lsp` for diagnostics, formatting, folding, symbol navigation,
rename, completion, signature help, inlay hints, and semantic highlighting.
Code actions can insert a missing `move` or take you to the borrow or move
that caused an ownership diagnostic.

`Kizu: Run File` and `Kizu: Test File` run the active `.kizu` file in an
integrated terminal. The commands also appear in the editor title and as
CodeLens actions above `fn main` and top-level `test` blocks.

## Requirements

The language server and the run and test commands call `kizu` from `PATH`.
Point them at another binary with `kizu.cli.path`:

```json
{
  "kizu.cli.path": "/absolute/path/to/kizu"
}
```

## Development

From the repository root:

```sh
just vscode-extension-check
```

To package a local VSIX:

```sh
just vscode-extension-package
```
