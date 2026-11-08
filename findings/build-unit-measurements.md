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
