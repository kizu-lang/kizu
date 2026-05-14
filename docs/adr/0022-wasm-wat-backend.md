# ADR-0022: Phase 11 の WASM backend は WAT 生成から始める

Status: 採用

## 背景

Kizu は LLVM だけでなく WASM / WASI への出力も持ちたい。
ただし Phase 11 の目的は、完全な WebAssembly toolchain ではなく、
typed SSA IR から WASI で実行できる形に下げられることを確認すること。

binary `.wasm` を直接生成すると、encoder、section layout、validation の実装範囲が広がる。
初期段階では、テキスト形式の WAT を生成し、`wasmtime` に validation と実行を任せる方が小さい。

## 決定

Phase 11 の WASM backend は WAT を生成する。

CLI は次の形にする。

```sh
kizu build --target wasm32-wasi <file>
```

生成する module は次を満たす。

- `_start` を export する
- memory を export する
- WASI `fd_write` を import する
- `print` は stdout に 1 行出力する
- `i64` は WebAssembly `i64` として扱う
- `bool` は `i32` として扱う
- `string` は linear memory の data segment として扱う

Phase 11 の target subset は Phase 2 examples に必要な範囲に限定する。

- function call
- local values
- integer arithmetic
- comparison
- `if`
- `while`
- `print`

## 影響

- `wasmtime` で Phase 2 examples の smoke test ができる
- WASM generation を build cache と性能測定の対象にできる
- binary `.wasm` emission は後続 phase に残す
- WASI filesystem、threads、browser integration は扱わない
- arena / handle の WASM lowering は後続 phase に残す
