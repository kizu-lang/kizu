# Kizu for Visual Studio Code

This extension registers `.kizu` files with syntax highlighting and snippets.
It also contributes `Kizu: Run File` and `Kizu: Test File` commands that run
the active `.kizu` file in an integrated terminal. The same commands are shown
in the editor title while a `.kizu` file is active, and as CodeLens actions
above `fn main` and top-level `test` blocks.

## Requirements

The run and test commands call `kizu` from `PATH`. Point them at another
binary with `kizu.cli.path`:

```json
{
  "kizu.cli.path": "/absolute/path/to/kizu"
}
```

There is no Kizu language server at the moment. The `kizu-lsp` server the
extension starts was part of the Go implementation and was removed with it,
so diagnostics and formatting through LSP are unavailable; the extension shows
an error when it cannot start the server.

## Development

From the repository root:

```sh
just vscode-extension-check
```

To package a local VSIX:

```sh
just vscode-extension-package
```
