# ADR-0153: C の struct は値でも渡し、呼び出し規約は compiler が守る

## 背景

ADR-0024 は `extern "c" struct` の値渡しを却下した。register への載せ方が
platform ごとに違い、pointer なら同じ意味になり、必要な例も無かったためである。

実際には option の束を値で受け取る C API があり、宣言できないと呼べない。
回避策は struct を register 幅の整数に割って渡すことだが、割り方は platform の
呼び出し規約そのもので、ADR-0024 が避けたかった platform 依存が利用者の
source に出てくる。

## 決定

* `extern "c" fn` は `extern "c" struct` を値で受け取り、値で返せる。
  綴りは通常の引数・戻り値と同じで、新しい構文も unsafe の種類も足さない。
* 値の渡し方は native target の C 呼び出し規約に従い、backend が clang と同じ形に
  書き換える(`internal/llvm/cabi.go`)。
  * arm64(AAPCS64、Darwin / Linux): float 1〜4 個の同型 aggregate は float register、
    16 byte 以下は整数 register 1〜2 本、それより大きいものは copy の address。
    16 byte を超える戻り値は `sret`。
  * x86-64(System V): 16 byte 以下は eightbyte ごとに整数 / SSE register。それより
    大きいもの、または残りの register に収まらないものは stack(`byval`)。
    16 byte を超える戻り値は `sret`。
  * それ以外の machine では値渡しを含む call を build error にする。
* target を持たない LLVM text(`kizu check`、`--emit-llvm`、corpus)は arm64 Linux の
  規則で書く。
* `extern "c" fn(...)` の関数 pointer 型は値渡しの struct を持てないままにする。
  値渡しの struct を取る C 関数は名前でだけ呼べ、値としては取り出せない。
* `export "c" fn` も struct を値で受け取らない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 値渡しを拒否したままにする | 呼べない C API が残り、回避策が platform 依存を source に持ち込む |
| C の shim を生成して clang に規約を任せる | build に C の compile が増え、生成物が見えない所で動く |
| 整数に割って渡す標準 helper | 割り方が platform ごとに違い、利用者がそれを選ぶことになる |
| 関数 pointer 経由の値渡しも同時に入れる | 間接 call と export 側の書き換えが要る。必要な例が出てから足す |
