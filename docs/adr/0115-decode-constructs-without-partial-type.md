# ADR-0115: decode は partial 型を持たず、comptime construct で組み立てる

Status: 採用

Issue: #1626

## 背景

encode v1(ADR-0112)と structural reflection(ADR-0113)で `encode<T>` は
入った。encode は値を **読む** だけなので、既存の field path borrow に
そのまま乗った。decode は値を **作る**。ここに今の言語には無いものがある。

JSON の key は宣言順で届かない。`{"age":30,"name":"a"}` も
`{"name":"a","age":30}` も同じ `User` になる。したがって decode は
**順不同で届く値から `T` を組み立てる場所** を要る。

置き場の候補は 3 つしかない。設計検証(#1626)で 3 つとも塞がっていることを
実行して確認した。

| 置き場 | 塞がり方 |
| --- | --- |
| partial struct に貯める | 当時 `?String` は struct field に置けず(#1632)、owner field は aggregate から move out できなかった(#1633)。どちらも入った今も、最後に `T` を作るところで literal の生成が要る点は変わらない |
| 呼び出し側の `T` を in-place で埋める | 初期化済み owner field への代入が黙って漏れる(#1630)。field の明示 `deinit` は owner の `deinit` 内でしか許されない |
| struct literal を comptime で生成する | `comptime for` は文であり、literal を作れない |

一方で、**具体型に対してなら組み立ては普通の Kizu code で書ける**。

```kizu
fn decode_user(allocator: Allocator, bytes: []u8, table: &var SpanTable) -> !User {
    let name = try decode_name(allocator, bytes, table);
    errdefer name.deinit();
    let age = try decode_age(allocator, bytes, table);
    return User { name: name, age: age };
}
```

これは ADR-0114(errdefer は move で退役する)が入れば通る形である。
足りないのは、この形を **型ごとに人間が書き写さずに済ませる** ことだけで、
それは原理 10 が畳めと言っている定型である。

## 決定

### 1. `partial<T>` を持たない

#1626 の初期案にあった `std::meta::partial<T>` は採らない。導出 struct、
その `deinit` の導出、`?Owner` field、aggregate からの move out の 4 つを
足しても、最後に `T` を作るところで結局 literal の生成が要る。置き場を
足すのではなく、置き場が要らない形にする。

### 2. `std::meta::construct<T, worker>` を足す

```text
std::meta::construct<T, worker>(args...) -> !T
```

`T` の public field を宣言順に組み立てて `T` を返す。各 field の値は
`worker<T, f>(args...)` の戻り値である。展開形は上の `decode_user` と
同じ形になる。

```kizu
// std::json 側
fn decode_struct<T>(allocator: Allocator, bytes: []u8, index: &var i64) -> !T {
    var table = try scan_object<T>(allocator, bytes, index);
    defer table.deinit();
    return try std::meta::construct<T, decode_field>(allocator, bytes, &var table);
}

fn decode_field<T, f>(allocator: Allocator, bytes: []u8, table: &var SpanTable)
    -> !std::meta::field_type<T, f> {
    // f の span を table から引き、field_type に応じて値を作る
}
```

```kizu
// construct<User, decode_field>(allocator, bytes, &var table) の展開
let name = try decode_field<User, name>(allocator, bytes, &var table);
errdefer name.deinit();
let age = try decode_field<User, age>(allocator, bytes, &var table);
return User { name: name, age: age };
```

- **errdefer は owner field にだけ並ぶ。** compiler は field 型の owner 判定を
  既に持っている(`ast.OwnerType`)。copy field に `deinit` は無い
- **ADR-0114 が前提。** errdefer が move で退役しなければ、生成した形自体が
  checker に拒否される
- **worker は `Function` static parameter(SPEC §13)。** closure ではなく、
  local を capture せず、top-level 関数名である。`<T, f>` は construct が補う。
  今の `Function` は非 generic な関数名を forward する用途なので、この補いを
  実装時に確定させる
  —— **確定した。** `f` は `Field` static parameter として宣言する
  (`fn decode_field<T, f: Field>(...)`)。owner はその直前に書かれた型引数で、
  `field_type<T, f>` がその順で書かれるのに合わせた。`Field` は型引数と同じく
  実体化キーになる(body が form を読むため)。`Function` と違い std 限定には
  しない —— construct を使った decoder を利用者が自分で書けないと、std が
  その利用者に対して特権を持つことになるため
- **runtime 引数は全 field に同じものが渡る。** field ごとの差は
  `field_name<T, f>()` と `field_type<T, f>` から worker 自身が読む
- **対象は public field を 1 つ以上持つ struct。** 持たない struct は compile
  error にする(encode と同じ理由。全部 private な型を組み立てたことにすると
  値が黙って消える)

原理 2 に照らす。呼び出し `std::meta::construct<T, decode_field>(...)` は source
にあり、worker の名前も source にある。生成された body でも呼び出しが見えて
いれば隠れていない。`Array.deinit_all` の要素ごとの cleanup と同じ位置づけである。

AGENTS.md の境界テスト(利用者が自分で同じものを書けるか)にも通る。具体型に
対しては書ける。form が畳むのは型ごとの繰り返しだけである。

### 3. cursor は struct にしない

parse 状態は `bytes: []u8` と `index: &var i64` の 2 引数で運ぶ。

view を field に持つ struct にすると詰む。view を捕捉した local は `&var`
引数として渡せず(ADR-0100 決定 2)、逃げ道になる generic method は
SPEC §13 が「実装しません」と決めている。2 引数に割れば view-carrying struct が
現れないので、`decode<T>(allocator: Allocator, bytes: []u8) -> !T` が
そのまま generic な free function になる。ADR-0100 の拡張は要らない。

### 4. `Value` は再帰 union、`Object` は `Array<Entry>`

```kizu
pub union Value {
    Null,
    Bool(bool),
    I64(i64),
    Str(std::string::String),
    Arr(std::array::Array<Value>),
    Obj(std::array::Array<Entry>),
}
```

`Map<[]u8, Value>` は使えない。map の value type は copy 限定である。
`Array<Entry>` は key 順を挿入順で保つので、#1626 の「Object を Map にするか
順序を保つ列にするか」はこれで決まる。

再帰 union は今日書ける。宣言も `&Value` の再帰 walk も通る。明示 `deinit` と
要素側の `deinit_all` が要る(ADR-0075)。ADR-0113 が「再帰的な所有データ
構造は今のところ作れない」と書いたのは struct の場合で、union には当てはまら
なかった。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `std::meta::partial<T>` に貯めて `finish<T>` で組み替える | 最後の `T` の literal 生成に construct と同じ form が要る。導出 struct が丸ごと余る(原理 6) |
| `decode_into<T>(..., out: &var T)` で呼び出し側の `T` を埋める | 初期値を用意する定型が call site の数だけ増える(原理 10)。owner field の入れ替えが漏れる(#1630)。失敗すると値が半分書き換わって残る(原理 1) |
| `Value` を経由して `from_value<T>` に詰め替える | 構築の壁は動かない。同じ壁の手前に経路が 2 本になる(原理 9) |
| `comptime construct<T> \|f\| { yield ... }` の文形 | `yield` という新しい制御が要る。`Function` static parameter は既にある(原理 6) |
| cursor struct を書けるよう ADR-0100 を広げる | 2 引数に割れば struct が要らない。言語を広げずに済む方を採る(原理 8) |
| `Value::Obj` を `Map<[]u8, Value>` にする | map の value type が copy 限定で不可 |

## 影響

- SPEC §13.1 に `std::meta::construct<T, worker>` を追加する
- `internal/stdmeta` に form を追加する。名前と arity は引き続きここが 1 つだけ持つ
- ADR-0114(#1634)が前提。先に入れる
- `std::json` に decode を足す。`docs/std/json.md` に API を書く
- #1626 の未決のうち「Object の表現」と「部分初期化状態の置き場」が閉じる

## 未決(#1626 に残す)

- error set の member をどこまで分けるか
- 深さ上限と入力サイズ上限の既定値
- number は v1 で i64 のみにするか
- 重複 key は error か後勝ちか
- surrogate pair を含む `\uXXXX` の扱い

## 再評価条件

- union / enum の decode が要るとき。`construct` は struct 専用で、variant の
  選択は別の形になる
- worker に field ごとの追加 static 引数が要るとき
- partial 経路が construct より単純になるかを問い直す。field の move out は
  入ったので、残る差は `T` の literal を誰が作るかだけである
