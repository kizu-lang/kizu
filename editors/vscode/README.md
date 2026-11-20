# Kizu for Visual Studio Code

This extension registers `.kizu` files with syntax highlighting and snippets,
starts `kizu lsp` for diagnostics, formatting, document symbols, and folding
ranges, and contributes `Kizu: Run File` and `Kizu: Test File` commands that
run the active `.kizu` file in an integrated terminal. The same commands are
shown in the editor title while a `.kizu` file is active, and as CodeLens
actions above `fn main` and top-level `test` blocks.

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
