# spec

実装より先に決めておく仕様と、その性質の証明です。Lean 4 で書き、実行できる
関数として定義するので、同じ file が trace も吐きます。

```
spec/Ledger.lean                  帳簿: 送金の意味、総和保存と再送の冪等性の証明、trace 生成
spec/lean-toolchain               elan が読む Lean の版
examples/fixtures/ledger_trace.json   生成した trace(checked in)
examples/ledger_conformance.kizu      trace を実装に流して食い違う手を名指しする program
```

仕様が持つのは 3 つです。**定義**(`Book.post`: 1 件の送金が状態をどう変え、
どの結果を返すか)、**性質の証明**(`total_reachable`: 到達可能なすべての状態で
残高の総和は開始時のまま。`retry_after`: 一度適用した送金は、間に何件挟んでも
再送は duplicate で状態を変えない)、**trace**(`main`: 乱択した送金列に対して
仕様が返す結果と各手の後の残高)。

実装側は trace を読んで 1 手ずつ進め、結果と残高を仕様と比べます。証明が保証する
のは仕様の性質で、実装がその仕様に従うことは trace の範囲での検査です。
食い違いは「どの trace の何手目で、何を期待し、何が返ったか」まで出ます。

trace は checked in なので、example を走らせるのに Lean は要りません。仕様を
変えた人だけが作り直します。

```sh
just spec-trace     # lean --run spec/Ledger.lean > examples/fixtures/ledger_trace.json
```

`lean` は [elan](https://github.com/leanprover/elan) が `spec/lean-toolchain` の版を
用意します。Mathlib は使いません。
