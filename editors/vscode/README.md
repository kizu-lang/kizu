# Kizu for Visual Studio Code

This extension registers `.kizu` files, starts `kizu-lsp`, and lets VSCode use
the language server for diagnostics and formatting.

## Requirements

Install `kizu-lsp` separately and make it available on `PATH`:

```sh
go install github.com/kizu-lang/kizu/cmd/kizu-lsp@latest
```

For local development from this repository, point VSCode at a checked-out
binary by setting `kizu.lsp.path`.

```json
{
  "kizu.lsp.path": "/absolute/path/to/kizu-lsp"
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
