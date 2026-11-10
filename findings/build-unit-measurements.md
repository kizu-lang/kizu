# build 再利用単位の計測(A0 / A1)

host: Apple M4 Max、Apple clang 17.0.0 (clang-1700.6.3.2)。数値は `just perf-cache` と
手元の 1 回測定。

## A0: 分割前

| 項目 | 値 |
| --- | --- |
| `compiler.ll` | 59MB / 773,429 行 / 6,377 関数 |
| clang -O0 `-c compiler.ll` | 2.22s(3 回とも 2.22〜2.24s) |
| `build --emit-llvm compiler`(front end + emit) | 1.80s |
| cold `run compiler` | 4.36s |
| warm `run compiler` | 1.79s |
| `internal/types` を 1 か所編集した `run compiler` | 4.38s(front end 1.76s + clang・link 約 2.6s) |

module ごとの IR 量(関数本体の行数、symbol 名からの集計。generic instance は別計上):

| module | 行 | 割合 |
| --- | --- | --- |
| compiler::internal::types | 143,128 | 18.8% |
| compiler::internal::wasm | 125,221 | 16.5% |
| compiler::internal::ir | 115,961 | 15.2% |
| compiler::internal::llvm | 106,533 | 14.0% |
| compiler::internal::ownership | 95,749 | 12.6% |
| generic instance 全体 | 27,896 | 3.7% |
| compiler::internal::parser | 25,601 | 3.4% |
| compiler::internal::project | 25,468 | 3.3% |
| その他(std 含む) | 残り | 各 2.4% 以下 |

最大 module(`internal/types`)は全体の 18.8%。使う generic instance を足しても
2 割強で、設計の見込み(2 割弱)とほぼ合う。1 module 編集で clang がやり直す量は
今の 2.2s の 2 割前後になる見込み。

## A1: debug build を module ごとの unit に分割した後

同じ host。compiler は 47 unit(46 module + program unit)に割れる。

| 項目 | A0 | A1 |
| --- | --- | --- |
| `internal/types` を 1 か所編集した `run compiler` | 4.38s | 2.77〜2.83s(3 回) |
| うち clang が compile し直す object | `compiler.ll` 全体 1 本(2.22s) | `internal/types` の unit 1 本(約 0.55s) |
| 全 object が hit し link だけする `run compiler` | ― | 2.21〜2.26s(front end + emit 約 1.8s + link 約 0.4s) |
| cold `run compiler`(`just perf-cache`) | 4.36s | 7.55s |
| warm `run compiler` | 1.79s | 1.83s |
| 1 編集あたりに増えた `native-obj` | ― | 1 個(47 → 50、3 回編集) |

1 module 編集の rebuild は 4.4s → 2.8s。clang の再 compile は全体の 2.2s から
編集した unit の 0.55s(25%)になり、A0 の見込み(2 割前後)とほぼ合う。

cold build は遅くなった(4.4s → 7.6s)。unit ごとに clang を 1 回ずつ順に起動するのと、
generic instance を使う unit の数だけ compile するため。A2(unit の並行 compile)が
これを縮める対象。

## A2: unit の並行 compile 後

| 項目 | A0 | A1 | A2 |
| --- | --- | --- | --- |
| cold `run compiler`(`just perf-cache`) | 4.36s | 7.55s | 3.44s |
| 1 module 編集の `run compiler`(`just perf-cache`、1 回) | 4.38s | 3.01s | 3.21s |
| Go 製 compiler が compiler を空 cache で build | ― | 5.1s | 2.7s |
| selfhost が自分を空 cache で build(bootstrap 第 2 段) | ― | 17.5s | 14.7s |

cold は A0 より速くなった。1 module 編集は clang 1 本なので並行化の影響はなく、
差は測定のばらつき(A1 の 3 回測定は 2.77〜2.83s)。
