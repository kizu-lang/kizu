# ADR-0122: `decode<T>` は所有する値だけを返す

Status: 採用

Issue: なし(#1653 の後続、`[]u8` の非対称の扱い)

## 背景

`encode` が受ける型と `decode` が返せる型は 1 つだけ違う。

```
$ kizu run view.kizu        # pub struct Tag { pub label: []u8 }
{"label":"hello"}

$ kizu check view_dec.kizu
error: comptime error: `std::json::decode_value` does not support type `[]u8`
```

`[]u8` を decode しない理由は #1653 で `docs/std/json.md` に入った。view は
document の連続した一部しか指せず、escape を戻した bytes はそこに無い。
escape の有無は parse するまで分からないので、型で決まる規則にできない。

残っていたのは「では非対称のままでよいのか」で、他言語は両方 decode できる。

**Zig** は runtime で分岐する。`ParseOptions.allocate` の既定は
`.alloc_if_needed` で、escape が無ければ入力への参照、あれば copy を返す
(`std/json/static.zig`)。寿命は `Parsed(T)` が持つ arena が引き受けるが、
`value` は入力 slice も指しうるので、入力を先に解放しても compiler は止めない。

**Rust** は型で分ける。`&'de str` は借用のみで、escape を含む文字列では失敗
する。両方受けるには `Cow<'de, str>` を使う。`Deserialize<'de>` の lifetime が
返り値を入力に縛る。

## 決定

**`decode<T>` は所有する値だけを返す。**`[]u8` は decode しない。文字列は
`std::string::String` の 1 本で受ける。

## 却下した代替案

| 案 | 却下理由 |
| --- | --- |
| **`Cow` 相当の型を足す** | Rust で `Cow` が実用になるのは `Deref` で `&str` として透過的に読めるから。Kizu に auto-deref は無く、hidden call を持たないのは原理 2 の中身なので足せない。読む綴りが `String` と別になり、両方を受ける関数が二重に要る(原理 10)。`[]u8` / `String` に続く 3 つ目の文字列型でもある(原理 6)。`decode` が返すと struct field を通って伝播し、言語の文字列表現が 2 系統になる(原理 9)。解けるのは escape の無い文字列の copy 1 回だけで、交換として割が悪い |
| **encode から `[]u8` を落として対称にする** | `write_bytes_field(name, string.as_bytes())` は文字列を JSON に入れる主経路で、失えない。encode は借りて書く側なので `[]u8` が正しい |
| **`decode` の返り値を arena 付き wrapper にする(Zig の `Parsed(T)`)** | `[]u8` field を安全に持てるが、値への経路が wrapper 越しに 1 段深くなる。ADR-0115 が DOM を却下したのと同型の理由 |

非対称は恣意ではなく役割の帰結である。`[]u8` は借用の綴り、`String` は所有の
綴りで、encode は借りて書き、decode は持たせて返す。受ける型が違うのは、両者が
値に対してすることが違うからである。

## 費用

所有一本にすると次を払う。どれも `std::json` 固有ではなく、所有モデルの費用で
ある —— `Array` / `Map` / `Box` を返す decode も同じものを払う。

1. **`deinit` の義務。**文字列 field を持つ struct は型ごとに `deinit` を
   書く。`[]u8` field なら要らない。ただし差が出るのは scalar と文字列だけの
   struct に限られ、container を持つ型はどのみち要る。
2. **文字列 field の数だけ allocation が走る。**Zig が `.alloc_if_needed` で
   省く分を省けない。std に「まとめて捨てられる allocator」は今のところ無い
   (`std::arena::Arena<T>` は typed arena で、`Allocator` として渡せない)。
3. **入力と copy を同時に持つ。**大きい document では効く。

## 再評価条件

unescape の copy が profile で decode の支配項になったとき。そのとき見るのは
`Cow` ではなく **`std` に reset できる allocator を足す方向**である。`Cow` は
文字列だけを狭い範囲で解くが、allocator なら上の費用 2 と 3 が消え、1 も軽く
なりうる。効く範囲が広い方から試す。
