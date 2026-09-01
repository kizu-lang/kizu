# ADR-0147: browser WASM は process を偽装せず byte stream を host 境界にする

Status: 採用

## 背景

WASI の `_start`、`fd_write`、process / filesystem capability は browser に存在しない。
それらを JavaScript で形だけ再現すると、利用できないものが build でき、load または
実行まで失敗が遅れる。任意の Kizu 関数を export すると関数の型ごとに別 ABI も要る。

## 決定

browser は独立した `wasm32-browser` target にする。portable lowering は WASI target と
共有し、target 差は import、entry / export、利用可能 capability だけが持つ。

host 境界は同期 byte write と program entry だけに閉じる。guest memory は callback 中の
borrow で、保持する host adapter が copy する。host が持たない std API と extern C は
build 時に target 非対応として拒否する。

この境界なら page を process に見せず、Kizu の値表現を JavaScript API として固定せずに
observable output と終了 status を渡せる。ABI の現在形は `docs/wasm-browser.md` が持つ。

## 却下した案

| 案 | 理由 |
| --- | --- |
| JavaScript で WASI を emulation する | 無い process / filesystem capability を有るように見せる |
| user 関数を自動 export する | source が export していない名前に答え、型ごとの payload ABI を固定する |
| linear memory の view を callback 後も保持する | `memory.grow` と guest の再利用で内容と lifetime が変わる |
| error を trap だけで表す | 回復可能な `main` error / `ExitStatus` と panic の差を失う |
