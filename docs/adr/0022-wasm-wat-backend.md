# ADR-0022: WASM backend は共通 module を WAT と binary に描画する

Status: 採用

## 背景

WASM backend は最初に WAT を生成し、typed SSA IR から WASI で実行できることを
小さい実装で確認した。browser は binary `.wasm` を必要とするが、WAT writer と binary
encoder が別々に IR を解釈すると、同じ target に 2 つの挙動が生まれる。

## 決定

typed SSA IR は 1 度だけ共通 WebAssembly module へ lower し、WAT と binary はその
module の renderer にする。binary は section と index space を deterministic に encode する。

互換な inspection 経路は WAT を stdout に出す。

```sh
kizu build --target wasm32-wasi <file>
```

`--emit wat` は renderer を明示し、`-o` があれば file に書く。binary は次の形だけを
受理し、terminal には暗黙出力しない。

```sh
kizu build --target wasm32-wasi --emit wasm -o app.wasm <file>
```

Go seed と shipping Kizu compiler は同じ bytes を出し、WAT / binary は同じ example の
observable behavior を `wasmtime` で検証する。

## 影響

- WAT は人間が検査でき、binary は外部変換 tool 無しで runtime と browser に渡せる。
- target subset は renderer 間で増減しない。
- browser 固有の import、entry / export、host adapter は別の target 判断として残る。

## 却下した案

| 案 | 理由 |
| --- | --- |
| WAT と binary がそれぞれ IR を lower する | renderer ごとに対応機能と失敗経路がずれる |
| binary bytes を stdout へ既定出力する | terminal を壊し、artifact の行き先が暗黙になる |
| WAT を廃止する | inspection と既存 CLI の互換経路を失う |
