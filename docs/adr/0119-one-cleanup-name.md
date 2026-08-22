# ADR-0119: cleanup の名前は `deinit` 1 つ(ADR-0091 の決定 3 を置換)

Status: 採用

Issue: #1626

## 背景

ADR-0091 決定 3 は「owner 要素の collection は shallow `deinit()` を型 error に
し、要素ごと consume する `deinit_all()` だけを consume と認める」と決めた。

当時の問題は「`Array<String>.deinit()` が要素を漏らす」であり、背景に
「examples に leak が 5 file ある」と書かれている。**別名は、漏れる操作を
compile error にするための手段だった。**名前が 2 つでなければならない理由は
ADR に書かれていない。

その 2 つが、要素型の決まっていない code を書けなくする。

```kizu
fn collect<E>(allocator: Allocator) -> !std::array::Array<E> {
    var items = std::array::new<E>(allocator);
    errdefer items.???();      // E が i64 でも String でも通る綴りが無い
    try items.reserve(1);
    return items;
}
```

`comptime if` で分岐しても効かない。branch は block なので、そこで登録した
`errdefer` は branch を出た時点で退役する。残る回避は「要素の種類ごとに同じ
関数を複製する」で、原理 10(定型が量産される設計は間違っている)に反する。

同じ理由で入れ子も書けなかった。`Array<Array<String>>` は「外側の cleanup が
要素の `deinit()` を呼ぶが、それは shallow で漏れる」ため構築時に型 error だった。

## 決定

**cleanup の名前を `deinit` 1 つにする。** `deinit_all` を廃止する。

`deinit` は値と、値が保持しているものを解放する。container なら要素を要素自身の
`deinit()` で consume してから buffer を解放し、何も保持しない要素ではその consume が
空になるだけである。

```kizu
fn (self: std::array::Array<T>) deinit<T>() -> void {
    comptime if std::meta::is_owner<T>() {
        while self.len() > 0 {
            let item = self.pop_or_panic();
            item.deinit();
        }
    } else {
    }
    std::internal::builtin::array_deinit<T>(self);
}
```

copy 要素の instance には loop が生成されない。

```text
fn std::array::Array.deinit.i64(%self: std::array::Array<i64>) -> void {
entry:
  array.deinit %self: std::array::Array<i64>, i64
  return void: void
}
```

ADR-0091 決定 3 の目的(要素の leak を compile 時に閉じる)は保たれる。漏れる
操作が無くなったので、別名で気づかせる必要そのものが消えた。原理 2 も満たす:
呼び出し `xs.deinit()` は source にあり、ADR-0091 決定 2 が「生成された body でも
呼び出しが source にあれば公理内」と定めている。

**`std::meta::is_owner<T>()` を足す。** body が要素 loop を出すかを comptime で
選ぶために要る。判定は `ast.OwnerType` —— checker が既に使うものと同じで、
二つ目の定義を作らない(原理 9)。std 限定にはしない。「この型は解放が要るか」は
generic code を書くすべての人が問う質問である。

**入れ子の制限が消える。** 1 つの名前は任意の深さに合成できる。

```kizu
var grid = array::new<array::Array<string::String>>(allocator);
defer grid.deinit();
```

2 つの名前では書けなかった。外側が「要素はどちらを受けるか」を選ぶ必要があり、
その答えが要素の要素にも依存するためである。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 2 つとも通す(`Array<i64>` に `deinit` と `deinit_all` の両方) | 同じことをする綴りが 2 つ並ぶ。読み手に無い区別を考えさせる(原理 6)。入れ子の制限も残る |
| 型ごとに見える method を変える(修正前の状態) | 型が決まっていない場所では「見える method」が決まらない。generic code を書けなくしている当の原因 |
| `comptime if` の branch が cleanup scope を作らないようにする | block はすべて scope という規則に例外を作る(原理 11) |
| `errdefer` が free function 呼び出しを受ける | cleanup の形が広がる。`errdefer f(x)` の x が receiver だと読めるかは綴りから決まらない |
| 要素の種類ごとに関数を複製する | 原理 10 そのもの |

## 影響

- SPEC §9: cleanup の名前が 1 つであることに書き換え。§13.1 に `is_owner<T>()`
- `internal/ast`: `CleanupMethodName` は常に `deinit`。`ContainerOwnsElements` を
  足し、lowering が「wrapper を呼ぶか runtime op か」をそこで決める
- `internal/types`: `cleanupChoiceError` を削除。method table から `deinit_all` を削除
- `internal/ownership`: 同上
- `internal/ir`: owner 要素の container だけ std wrapper を呼ぶ。plain 要素は
  従来どおり runtime op 1 つ
- `lib/kizu/std/src/{array/array,mem/mem}.kizu`: `deinit` body の comptime 分岐
- negative example 3 本が合法になり削除(shallow deinit 2、入れ子 1)。入れ子は
  positive example に置き換え
- `.kizu` 26 file の `deinit_all` を `deinit` に書き換え

## 再評価条件

- `deinit` が何を解放するかを名前から読めないことが実需の問題になったとき。
  要素型は宣言に書いてあるので、今のところ追える
- `is_owner` の判定が checker の owner 性と食い違う必要が出たとき。今は同じ
  定義を読んでいる
