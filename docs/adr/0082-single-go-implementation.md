# ADR-0082: 実装は Go 一本にし、selfhost は言語が固まってから作り直す

Status: 採用

Supersedes: ADR-0080(フル Kizu selfhost)、ADR-0081(自己コンパイル backend の撤去)

## 背景

ADR-0081 で自己コンパイル backend を削除し、selfhost は 164,011 行から 73,260 行に
なった。それでもなお、次の状態が残っていた。

| | 行数 |
| --- | ---: |
| selfhost | 73,260 |
| Go 実装(test 除く) | 42,738 |
| Go テスト | 29,561 |

**第二実装が参照実装より大きく、しかも機能は劣っていた。** `cmd/kizu` の
テストファイル 52 個のうち 48 個が selfhost に依存しており、テスト労力の大半が
言語ではなく第二実装に向いていた。言語機能を足すたびに 2 回書く必要があり、
comptime はその途中で止まっていた(Go は実装済み、selfhost は check 段のみ)。

一日で見つかった不具合の質も、この構造を示していた。手書き LLVM がソースと違う
意味論を持ち、存在しない entry を叩く gate が `err != nil` を成功と誤認して
緑を保ち、`stage2` の挙動がソースと乖離していた。いずれも**二重実装そのものが
生む失敗モード**である。

一般的な言語実装は、例外なく self-host を後回しにしている。

| 言語 | 最初の実装 | self-host |
| --- | --- | --- |
| Rust | OCaml | 2011(言語が固まってから) |
| Zig | C++ | 2022 / 0.10(約 5 年後) |
| Go | C | 2015 / 1.5(約 6 年後) |
| Crystal | Ruby | 早期に移行し、長くビルド問題を抱えた |

Kizu は SPEC がまだ動いている。動く仕様を 2 つの実装で追いかける余裕はない。

## 決定

### 1. `selfhost/` を削除し、実装は Go 一本にする

言語・ツールチェインの正は `internal/` + `cmd/kizu`。CLI の `check` / `parse` /
`run` / `test` / `fmt` はすべて Go 実装が持つ。

### 2. 言語のテストは examples + conformance が持つ

`examples/` の 373 本と `cmd/kizu/conformance_test.go` の manifest が、全 example を
カバーすることを強制する。parity gate は不要になる。

### 3. `std/src/kizu/*` は残す

`std::kizu::{lexer,parser,ast,diagnostic}` は std の一部であり、Go の checker が
`internal/stdlib` 経由で取り込む。Kizu が Kizu を記述する層はここで小さく保つ。

### 4. self-host は言語が固まってから、Go の構造に沿って作り直す

再開の条件は、SPEC が安定し、Go 実装が仕様を満たしていること。作り直すときは
ADR-0081 の結論に従う: op を持つ汎用命令 1 種、AST を歩く lowering、命令 1:1 の
renderer。ソースの形ごとの payload 型・関数名分岐・形状 template・手書き LLVM は
作らない。

## 影響

- 削除: `selfhost/` 全体と、それを対象とする Go テスト 45 ファイル。
  合計 108,958 行(642 files changed)。
- `cmd/kizu` のテストファイルは 52 → 7。テストは言語だけを見る。
- CI は 1 job(`go test ./...` + gofmt)になる。parity job は対象ごと消えた。

## 削除が明らかにした Go 側の欠陥

selfhost が覆い隠していたものが出てきた。すべてこの ADR と同じ変更で直した。

- `kizu fmt` が**行末コメントを捨てていた**。`internal/fmt` は行頭コメントしか
  収集しておらず、`code // comment` の comment は消えていた。文字列リテラルを
  避けて `//` を探し、コメントは行頭・行末どちらも独立行として保存する。
- `kizu fmt` が**構文エラーを通していた**。token ベースなので壊れたソースも整形
  できてしまう。整形前に parse し、通らなければ拒否する。
- `kizu fmt` の**出力が再パースできなかった**。match arm 末尾の `,` は文法上必須
  なのに、閉じ括弧直前の `,` を落とす規則が消していた。enum 用にあった復元処理を
  match にも効く形へ一般化した。
- **package 名でルート名前空間を参照できなかった**。ADR-0049 は
  `[package].name` を package root namespace と定めているが、Go の checker は
  import されたものしか名前解決していなかった。import 集合(依存辺)と名前解決
  集合を分け、後者に root を含める。前者に入れると全 package が cycle になる。
