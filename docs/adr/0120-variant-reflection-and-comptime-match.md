# ADR-0120: variant の reflection と `comptime match`

Status: 採用

Issue: #1649

## 背景

ADR-0113 は structural reflection の v1 を struct の `pub` field に限り、
「enum tag / union variant は別 issue とする」と切り出した。再評価条件には
「union / enum の reflection が要るとき、`public_fields` と同じ規則で variant
列挙を足す」と書いてある。

要るようになった。`std::json::encode<T>` / `decode<T>` は generic な walk で、
union と enum に当たると `std::meta::unsupported` で止まる。struct が union
field を 1 つ持つだけで、その struct 全体が generic 経路から外れる。

列挙・名前・payload 型は `public_fields` の鏡で足りる。足りないのは 1 つ、
**値が今どの variant かでの分岐**である。struct には要らなかった —— field は
全部そこにあるので歩けばいい。variant は 1 つしか無く、どれかは runtime にしか
分からない。

## 決定

### 1. variant 側の form を struct 側の鏡として足す

| variant | struct |
| --- | --- |
| `is_enum<T>()` / `is_union<T>()` | `is_struct<T>()` |
| `variants<T>()` | `public_fields<T>()` |
| `variant_name<T, v>()` | `field_name<T, f>()` |
| `variant_type<T, v>` | `field_type<T, f>` |
| `has_payload<T, v>()` | `has_public_fields<T>()` |
| `variant<T, v>(payload)` | `construct<T, worker>(args...)` |

`variants<T>()` は `comptime for` の list で、順序は source の宣言順。
`public_fields` と同じ規則である。

`is_enum` と `is_union` を分けたのは、enum と union が別の宣言だからである
(SPEC §6.7、§6.8)。「payload を持てない sum」と「持てる sum」は別の質問で、
畳むと片方しか受けない code が書けなくなる(原理 7)。

### 2. 分岐は `comptime match`。展開されるのは `match` そのもの

```kizu
comptime match value |v, payload| {
    comptime if std::meta::has_payload<T, v>() {
        try encoder.begin_object();
        try encode_field<std::meta::variant_type<T, v>>(
            encoder, std::meta::variant_name<T, v>(), payload);
        try encoder.end_object();
    } else {
        try encoder.write_bytes(std::meta::variant_name<T, v>());
    }
}
```

variant ごとに 1 arm、宣言順、payload を持つ variant では第 2 capture が
その arm の payload binding になる。

```kizu
// union Shape { Point, Circle(i64) } に対して上が表すコード
match value {
    Point => { ... },
    Circle(payload) => { ... },
}
```

**新しい分岐機構ではない。**展開の結果が `match` なので、exhaustiveness も
payload の借用も所有権も §6.12 の規則がそのまま効く。`ast.ComptimeMatchExpansion`
が 1 箇所で arm を組み、types / ownership / ir がそこを通る(原理 9)。

payload の借用に form を足さなかったのはこのためである。arm の binding が
そのまま `payload` なので、borrow 追跡も move も既存の match の規則が答える。

### 3. JSON の wire format は external tagging

| 形 | JSON |
| --- | --- |
| payload を持たない variant | `"Point"` |
| payload を持つ variant | `{"Circle": 10}` |

enum は「全 variant が payload を持たない場合」として同じ経路に落ち、
`Color::Red` は `"Red"` になる。enum 専用の分岐は要らない(原理 9)。

`std::json::Value` だけは例外で、tag を付けずに書く。`Value` の variant は
program の型ではなく JSON 自身の形を名指しているので、tag で包むと「自分が何か
既に言っている document」を二重に包むことになる。decode 側が手書きの
`decode_any` であるのと対になる。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `visit<T, worker>(value, args...)`(`construct` の鏡) | worker の arity が variant ごとに変わる。payload を持たない variant のために unit 型を足すことになり、原理 6 に反する |
| `is_variant<T, v>(value)` + `payload<T, v>(value)` を `comptime for` の中で使う | 活性でない variant の payload を借りられてしまう。`if` で守られていることを compiler は知らないので、型の取り違えが通る(原理 1) |
| payload を `?&P` で返す | 借用の optional という型の形が増える(原理 6)。payload を持たない variant には返す型そのものが無い |
| `comptime match` の arm を payload 有無で 2 つに分ける構文 | 文法が増える。`comptime if has_payload` が既に同じことを表す |
| adjacent tagging(`{"tag": "Circle", "value": 10}`) | enum が `{"tag":"Red"}` になる。誰も欲しくない形なので enum 専用分岐が要り、経路が 2 本になる |
| union の tag だけ variant を `{"Point": null}` に揃える | payload を持たない variant が payload `null` を持つことになる。`?T` payload と区別がつかない |

## 影響

- SPEC §13.1 に variant の form 6 つと `has_public_fields`(記載漏れ)を追加。
  §13.2 に `comptime match`。「enum tag と union variant の列挙は持ちません」を
  削除
- `internal/ast`: `ComptimeMatchStmt`、`ComptimeMatchExpansion`、
  `VariantExpansion`。`MatchStmt` に `MetaCapture` / `MetaOwner`
- `internal/parser`: `comptime match value |v, p| { ... }`
- `internal/stdmeta`: form 7 つと `VariantForm`
- `internal/types` / `internal/ownership` / `internal/ir`: capture が field か
  variant かを持ち、match の arm を回すときに arm 自身の variant を束縛する
- `lib/kizu/std/src/json/json.kizu`: `encode_variant_value` / `encode_variant_field` /
  `decode_variant`、および `Value` の `encode_any_value` / `encode_any_field`
- `docs/std/json.md`: encode / decode の表に union と enum

## 再評価条件

- `comptime match` を式として書きたい実需が出たとき。今は statement だけで、
  arm の値を返す形は持たない
- variant を worker へ渡したくなったとき。`Field` static parameter に対応する
  `Variant` static parameter が要る。今は `comptime for` の中に閉じている
- untagged union の decode が要るとき。今は tag が document に書いてある形しか
  読まない
