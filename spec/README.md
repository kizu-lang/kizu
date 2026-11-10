# spec

実装より先に決めておく仕様と、その性質の証明です。Lean 4 で書き、実行できる
関数として定義するので、同じ file が trace も吐きます。

```
spec/Ledger.lean                  帳簿: 送金の意味、総和保存と再送の冪等性の証明、trace 生成
spec/Agreement.lean               契約: 状態と事象の遷移表、終了の吸収性などの証明、trace 生成
spec/Tls.lean                     TLS 1.3 の handshake: std::tls から生成した遷移表、順序の証明、trace 生成
spec/tls_machine.kizu             std::tls の全組を読んで表を JSON で出す program
scripts/machine-lean              その JSON を spec/Tls.lean の生成の block に書く道具
spec/tls_client.qnt, tls_server.qnt  同じ JSON から scripts/machine-quint が作る Quint の機械
spec/tls_properties.qnt           その機械が守る性質(手書き)
spec/ir/                          internal/ir の slot 判定・index check・defer 配置の Quint モデルと、lowerer に当てるプログラムの生成(README.md)
spec/ownership/                   ownership checker の Quint モデルと、checker に当てるプログラムの生成(README.md)
spec/unsafecap/                   `unsafe` マーカー検査の Quint モデルと、checker に当てるプログラムの生成(README.md)
spec/cimport/                     C header import の Quint モデルと、importer と checker に当てるプログラムの生成(README.md)
spec/lean-toolchain               elan が読む Lean の版
examples/fixtures/*_trace.json    生成した trace(checked in)
examples/*_conformance.kizu       trace を実装に流して食い違う手を名指しする program
```

`Agreement.lean` は遷移表そのものが仕様で、表に無い事象は refused として状態を
変えません。証明は `ended_absorbing`(終了した契約は何を送っても終了のまま)、
`active_only_by_acceptance`(active になるのは offered からの accept か suspended
からの resume だけ)、`terminate_needs_agreement`(terminate が効くのは active か
suspended だけ)です。

`Tls.lean` は RFC 8446 §A.1 の client と §A.2 の server の状態機械です。client では、
表に無い message は
`unexpected_message` で接続を閉じ(`refusal_closes`)、閉じた接続には何も起きず
(`closed_absorbing`)、connected になるのは待っていた `Finished` によってだけ
(`connected_only_by_finished`)、application data は connected でだけ受け取り
(`data_needs_connection`)、`CertificateVerify` は `Certificate` の後にだけ来ます
(`verify_needs_certificate`、`certificate_first`)。server は同じ形の性質を
`server_` で始まる名前で持ちます。

`Tls.lean` の表は手で書きません。`-- BEGIN GENERATED` と `-- END GENERATED` の間は
`std::tls` の `advance` と `advance_server` の全組から作るので、証明は実際に動く表に
ついてのものです。`go test` は block が今の `std::tls` と同じことを Lean 無しで確かめ、
定理は手で書いたままなので、性質を壊す変更は `lean` で落ちます。その代わり、RFC と
照らす独立した表ではなくなりました。誤った変更を止めるのは定理の範囲までです。
`examples/tls_conformance.kizu` が trace を流します。

仕様が持つのは 3 つです。**定義**(`Ledger.lean` なら `Book.post`: 1 件の送金が
状態をどう変え、どの結果を返すか)、**性質の証明**(`total_reachable`: 到達可能な
すべての状態で残高の総和は開始時のまま。`retry_after`: 一度適用した送金は、間に
何件挟んでも再送は duplicate で状態を変えない)、**trace**(`main`: 乱択した列に
対して仕様が返す結果と各手の後の状態)。

実装側は trace を読んで 1 手ずつ進め、結果と残高を仕様と比べます。証明が保証する
のは仕様の性質で、実装がその仕様に従うことは trace の範囲での検査です。
食い違いは「どの trace の何手目で、何を期待し、何が返ったか」まで出ます。

trace は checked in なので、example を走らせるのに Lean は要りません。仕様を
変えた人だけが作り直します。

```sh
just spec-table     # std::tls を読んで spec/Tls.lean の block と Quint の機械を作り直し、spec-verify を流す
just spec-verify    # 到達できる全部の状態で Quint の性質を TLC で確かめる(Java が要る)
just spec-quint     # Quint のランダムな実行で性質を探す(Java は要らない)
just spec-trace     # lean --run spec/<Name>.lean > examples/fixtures/<name>_trace.json
just spec-ir        # internal/ir モデルの quint test と性質の探索、internal/ir の fixture の再生成
just spec-ownership # ownership モデルの quint test と健全性の探索、internal/ownership の fixture の再生成
just spec-unsafecap # unsafe マーカーモデルの quint test、健全性と mutant の探索、internal/unsafecap の fixture の再生成
just spec-cimport   # C header import モデルの quint test、健全性と mutant の探索、internal/cimport の fixture の再生成
```

`go test` も CI も Lean と Quint を動かしません。対応する箇所を変えた人が手元で回します
(`AGENTS.md`)。

Lean と Quint は役割が違います。Lean は上限のない列について証明し、Quint は破れた時に
そこへ至る手順を反例で出します。`spec-verify` は `quint verify --backend=tlc` で、到達できる
全部の状態を調べます。Java は `PATH` のものか nixpkgs の JDK を使い、どちらも無ければ
失敗します。既定の Apalache の backend は同じ検査が 8 分で終わらなかったので使いません。
`Connected` に届かない機械は `Connected` についての性質を素通りするので、`notConnected` が
破れなければ検査は失敗します。

`spec-quint` はランダムな探索で、全数の検査ではありません。半分の手は終わりへ行かない
事象から選ぶので、実行は `Connected` まで届き、届いた実行が無ければ検査は失敗します。

`lean` は [elan](https://github.com/leanprover/elan) が `spec/lean-toolchain` の版を
用意します。Mathlib は使いません。
