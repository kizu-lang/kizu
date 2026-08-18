# ADR-0107: match arm body に block を許す

Status: 採用

Issue: #1593

## 背景

match arm body は expression または `return` 文で(ADR-0093)、block は
「この必要に対して過大」として却下されていた。その結果 2 つの問題が残った。

1 つ目は「何もしない」を綴る形がないこと。SPEC は `Tag => return,` を
「何もしないを明示する書き方」と説明していたが、`return` は囲む関数からの
早期 return であり、両者が一致するのは match が tail position にあるときだけ。
loop 内に同じ arm を書くと型検査も借用検査も通ったまま関数ごと抜け、
実行結果だけが黙って誤る。

2 つ目は call site の定型量産。arm に複数の処理や代入を書けないため、
arm ごとの helper 関数や no-op 関数の定義が call site 側に増える。
これは原理 10(定型が量産される設計は間違っている)に当たる。

さらに Kizu は if expression の分岐に既に block + 末尾 value を持っている
(ADR-0074)。「if の枝は block を書けるが match の arm は書けない」は、
同じ「式の枝」に別の規則を課す非対称だった(原理 7)。

## 決定

match arm body に block を許す。ADR-0093 の却下案
「arm に block `{ ... }` を許す」を覆す。

- **statement match** の arm block は文の並び。他の block と同じ規則で
  `let` / 代入 / `defer` / `return` / `break` / `continue` を書ける。
  空の `{}` は「この arm は何もしない」を明示する no-op。

```kizu
match kind {
    A => {
        count = count + 1;
        print(count);
    },
    B => {},
}
```

- **expression match** の arm block は if expression の分岐 block と同一の
  規則(ADR-0074): 末尾を value で終える。末尾が `;` の block と空の `{}` は
  error(「expression block must end with a value」)。`return` arm は
  従来どおり statement match のみ(ADR-0093)。

```kizu
let label = match color {
    Red => {
        let tag = "r";
        tag
    },
    Green => "green",
};
```

- arm の terminal comma は block でも必須のまま(SPEC §6「すべての arm は
  terminal comma を必須」を維持)。
- AST は専用 node を作らず `*ast.BlockStmt` をそのまま arm body に置く。
  checker / lowering は if の分岐が使う既存の block 経路
  (checkBlock / checkBlockValue / lowerBlock / lowerBlockBody)に流す。
  bare block statement が現れるのは match arm body だけ。
- owner-payload union の deinit 契約(ADR-0075、ADR-0091)が受理する
  cleanup arm は `Kept(payload) => payload.deinit(),` の直接形のまま。
  block 包みは契約 error にし、error message が直接形を提示する。
  契約の形が 1 つであることが grep / review の前提のため。
- union の owned payload を bind して空 arm で捨てる形
  (`Kept(payload) => {},`)は借用検査の leak error になる(既存の
  consume 検査がそのまま働く)。

あわせて SPEC §6.12 の `Tag => return,` の説明を改め、「何もしない」は
match の後に実行される文がない位置に限って成り立つ見かけであること、
no-op の意図には `{}` を使うことを明記する。

## ADR-0093 の「値返しへの押し出し」について

ADR-0093 は「arm に代入を書けないことで、効果を値として返す形に押し出される」
を block 却下の利点としていた。本 ADR はこの強制を grammar から外す。
押し出し自体は style として書き続けられるが、強制の対価が no-op 関数と
arm ごとの helper 関数という定型量産(原理 10)であり、if expression との
非対称も残るため、強制をやめる判断をユーザーが行った。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 空の `{}` だけ許す(本 issue の当初案) | 罠は消えるが定型量産と if との非対称が残る。中途半端な特殊形 |
| `B => (),`(unit 値) | SPEC §7「void は値ではない」を覆す |
| `B => ,`(空 body) | 前例が Go の `case B:` のみ。Rust / Zig 由来の手は `{}` に伸びる |
| SPEC の記述修正のみ | 誤誘導は消えるが、no-op を綴る形が存在しない |
| deinit 契約でも block 包み cleanup を受理 | 契約の受理形が 2 つになる。直接形は error message が提示する |

## 影響

- parser: `=>` 直後の `{` は block として parse
- project(qualify): bare block statement の namespace 解決経路を追加
  (従来は silent pass-through で、arm block 内の import 使用が
  unused import 誤検出になっていた)
- types / ownership / ir: statement 位置は checkBlock / lowerBlock、
  expression 位置は checkBlockValue / lowerBlockBody の既存経路に接続
- SPEC §6.12 更新
