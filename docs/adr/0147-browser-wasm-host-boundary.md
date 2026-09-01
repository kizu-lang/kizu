# ADR-0147: browser WASM は明示的な import / export を host 境界にする

Status: 採用

## 背景

WASI の `_start`、`fd_write`、process / filesystem capability は browser に存在しない。
それらを JavaScript で形だけ再現すると、利用できないものが build でき、load または
実行まで失敗が遅れる。一方、byte stream と program entry だけでは DOM、timer、Fetch
など browser 固有 capability を Kizu program が要求する方法も、非同期完了時に host が
guest へ戻る source 上の入口もない。

## 決定

browser は独立した `wasm32-browser` target にする。portable lowering は WASI target と
共有し、target 差は import、entry / export、利用可能 capability だけが持つ。stdout /
stderr は既存の同期 `kizu.write`、program entry は `kizu_start` のままにする。

利用者が追加する JavaScript capability は `extern "browser" fn` で `host` module から
同期 import し、host からの入口は `export "browser" fn` だけを明示 export する。
scalar / raw pointer と import parameter の `[]u8` に ABI を閉じ、aggregate、owner、borrow、
error union を関数ごとの payload に変換しない。guest memory は import call 中の borrow で、
保持する host がその場で copy する。

import は suspend しない。非同期 API は handle を渡して開始し、JavaScript が後で明示
export callback を status とともに呼ぶ。bytes は callback が guest-owned storage を用意し、
同期 import で読む。これにより制御フロー、storage、失敗の変換が source に残る。

host が持たない std API と extern C は build 時に target 非対応として拒否する。ABI の
現在形は `SPEC.md`、adapter の利用法は `docs/wasm-browser.md` が持つ。

## 却下した案

| 案 | 理由 |
| --- | --- |
| JavaScript で WASI を emulation する | 無い process / filesystem capability を有るように見せる |
| user 関数を自動 export する | source が export していない名前に答え、型ごとの payload ABI を固定する |
| import が `Promise` を返して Kizu の実行を止める | suspend / resume と lifetime が source から消える |
| aggregate / owner / error union を自動変換する | source の形ごとの payload ABI と hidden allocation が増える |
| compiler が DOM / Fetch の名前を特別扱いする | browser capability と関数名の対応が source から消える |
| linear memory の view を callback 後も保持する | `memory.grow` と guest の再利用で内容と lifetime が変わる |
| error を trap だけで表す | 回復可能な `main` error / `ExitStatus` と panic の差を失う |
