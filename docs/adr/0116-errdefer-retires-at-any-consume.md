# ADR-0116: errdefer は consume 全般で退役する(ADR-0114 決定 3 の訂正)

Status: 採用

Issue: #1626(decode の設計検証の続き)

## 背景

ADR-0114 は「errdefer は receiver が move された時点で退役する」を決め、
決定 3 で「explicit `deinit` と borrow は従来どおり error のままにする」と
書いた。理由は「`errdefer x.deinit()` の隣に `x.deinit()` を書くのは義務の
二重履行で、ほぼ確実に書き手の誤り」である。

これは誤りだった。次は正しいコードである。

```kizu
fn place(allocator: Allocator, parent: &var array::Array<string::String>, ok: bool) -> !void {
    var name = string::new(allocator);
    errdefer name.deinit();
    try name.append_byte(cast<u8>(97));
    if !ok {
        name.deinit();
        return PlaceError::Rejected;   // errdefer が走ったら二重解放
    }
    try parent.append(name);
    return;
}
```

「先に手放してから error を返す」は普通の形である。ここで errdefer を
実行してはならない。`deinit` は義務を果たしたのだから、そのあとの error exit
で同じ値をもう一度解放するのは二重解放である。

実装も ADR-0114 決定 3 のとおりにはなっていなかった。`deinitialized` flag を
立てるのは `std::arena::Arena` の `deinit` だけで、`String` / `Array` /
`Map` / `Box` の `deinit` は `moved` を立てる。結果として、**同じ形が
String では通り、Arena では拒否される**という状態になっていた。

```text
// String: check ok、IR も error return に cleanup を出さない
// Arena : move error: errdefer cleanup receiver `users` was deinitialized before an error path
```

決定 3 は、この不一致を「Arena が正しく String が漏れている」と読み違えた
ところから来ている。実際には逆で、String が正しく Arena が過剰拒否していた。

## 決定

**errdefer entry は receiver が consume された時点で退役する。** move でも
explicit `deinit` でも同じである。どちらも値はもう無く、cleanup を実行すれば
この frame が持っていないものを解放することになる。

ADR-0114 の決定 3 のうち、explicit `deinit` を error に保つ部分を取り消す。
決定 1・2・4 と、borrow の扱いはそのまま残す。

**borrow は error のまま。** 意味が違うからである(原理 7)。move と deinit は
義務が「済んだ」状態で、borrow は値がまだ生きていて consume できない状態を指す。
借用中の値に cleanup を走らせることはできないので、退役ではなく拒否が正しい。

冗長な `errdefer x.deinit(); x.deinit();`(間に exit が無い形)への lint は
持たない。上の「早期解放 + error return」と綴りが同じで、区別するには
「この deinit までに error exit があるか」を見る必要がある。安全性の問題では
ないので、閉じたまま出す(原理 8)。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| String 側を Arena に合わせて error にする(ADR-0114 決定 3 の実装) | 上の `place` のような正しいコードを拒否する。実際に Arena では拒否されていた |
| 「全 path で explicit deinit されたときだけ error」にして lint を残す | 3 つ目の状態と分岐 merge の規則が要る。得られるのは安全性に関わらない冗長性の指摘 1 つで、割に合わない |
| `deinitialized` flag を非 Arena にも広げてから判定する | flag の診断語彙が arena 専用(`arena error: arena X was deinitialized`)で、String に流用すると誤った message が出る。判定を consume 一本にすれば flag を広げる必要がない |

## 影響

- SPEC §6.3.1 の errdefer 退役条件を「move」から「consume」に広げる
- `internal/ownership`: `validateErrDeferReceivers` が move と deinit の両方で
  退役する。borrow の拒否は残す
- `internal/ir`: 変更なし。退役は checker が記録した `RetiredErrDefers` を
  読むだけで、判定は checker 1 箇所にある(ADR-0114 決定 1)
- Arena の過剰拒否が消える。ownership test の 2 例が positive 側へ移る

## 再評価条件

- 冗長な errdefer の lint が実需になったとき。「この deinit までに error exit が
  あるか」を見る形で足せる
- borrow 中の errdefer receiver に、拒否以外の答えが要るとき
