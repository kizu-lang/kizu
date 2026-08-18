# ADR-0106: `&var` receiver の two-phase borrow

Status: 採用

Issue: #1585

## 背景

`&var self` を取る method の呼び出しで、引数の中で `self` を読むと
拒否されていた:

```kizu
fn (self: &var Registry) advance() -> void {
    self.select(self.cursor);   // borrow error: `self` cannot be read
}
```

検査は call 式の評価順どおり receiver の可変借用を先に活性化していたため、
引数側の read が「可変借用中の read」に当たった。回避策はローカルへ写す
1 行だが、意味も生成コードも変わらず、借用検査を通すためだけの儀式で、
SPEC §20 の「儀式が少ない」から外れる。自分の状態を読んで自分を進める
`self.step(self.pending)` の形は状態機械で頻出する。

一方、field 経由の同形(`self.entries.append(self.cursor)`)は field 単位の
disjoint 判定で既に通っており、粗いのは receiver 全体を取る method call の
1 点だけだった。

## 決定

method call の `&var` receiver の可変借用を two-phase にする:

1. receiver 位置では可変借用を**予約**する(共有借用と同じ扱い)
2. 引数式の評価中、receiver は read できる
3. 引数がすべて確定した時点で排他借用を**活性化**し、call する
4. 引数が receiver を borrow / move する場合は従来どおり拒否する
   - 引数内の `&var self` method 呼び出し(`self.m(self.n())`)は
     予約(共有借用)と衝突して拒否
   - receiver に重なる borrow 引数(`self.m(&self.cursor)`)は call まで
     生きるため、活性化時に排他借用と衝突して拒否

適用範囲は method call の receiver のみ。関数引数の明示 `&var`
(`f(&var x, x.cursor)`)は従来どおり即時活性化のまま拒否される。
receiver は言語が挿入する借用で利用者が書いた `&var` 式ではなく、
書き分けの余地がないため、精度を上げても読み手が追う情報は増えない。

## 却下した代替

- **現状維持(ローカルへ写す規律)**: 検査は単純なまま残るが、写す行は
  「借用検査を通すために書いた」以外の情報を持たず、呼び出しのたびに増える。
- **明示構文で予約を書かせる**: 活性化タイミングは制御フローではなく検査の
  内部事情で、利用者に書かせる情報ではない。ADR-0098 が `borrows` 節を
  「導出可能な情報を人間に書かせる儀式」として削除したのと同じ判定。

## 帰結

- `self.select(self.cursor)` が通る。今まで通っていたコードは意味も
  診断も変わらない。
- Kizu の非目標「複雑な lifetime programming」は lifetime の記述について
  であり、借用領域の精度は field 単位の disjoint 判定に続いて receiver の
  活性化タイミングでも実挙動に揃った。到達点は Rust の two-phase borrow と
  同じだが、対象は method receiver に限る。
