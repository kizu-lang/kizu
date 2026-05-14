# ADR-0026: contract / satisfy / Dyn は明示的な抽象化として扱う

Status: 採用

## 背景

Kizu は抽象化を完全に捨てない。
ただし、Rust trait system の完全再現や、Go interface のような暗黙適合は目指さない。

Kizu の抽象化は、人間がレビューしやすく、dynamic dispatch が見える形に保つ必要がある。

## 決定

Kizu v0.1 では contract system を実装対象にする。
ただし、Rust trait system の完全再現はしない。

Kizu の抽象化は次の3つに分ける。

```text
contract  型が満たすべき要求
satisfy   型が contract を満たすことの明示宣言
Dyn       runtime dynamic dispatch を見せる型
```

## contract

`contract` は required method signatures だけを持つ。
method body は書けない。

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> !i64
}
```

## satisfy

`satisfy` は明示適合だけを表す。
Go のような暗黙 interface 適合は採用しない。

```kizu
satisfy Writer for File
```

`satisfy` block 内に method body は置かない。
method body は `impl Type` に置く。

## Dyn

`Dyn<Contract>` は dynamic dispatch を型に見せる。

```kizu
fn save(writer: borrow Dyn<Writer>, bytes: borrow Bytes) -> !void {
    let n = writer.write(bytes)
    return void
}
```

`Dyn<Writer>` と書かれている場所では runtime vtable dispatch が発生してよい。

## 他言語との差分

Rust trait との差分:

- trait system の完全再現をしない
- impl block に contract method body を置かない
- dynamic dispatch は `Dyn` で明示する

Go interface との差分:

- 暗黙適合を採用しない
- `satisfy` で適合を明示する

Zig 手書き vtable との差分:

- vtable pattern を手作業だけにしない
- `Dyn<Contract>` で動的ディスパッチの型境界を表す

## 影響

- 抽象化の意図がコード上に残る
- dynamic dispatch が隠れない
- v0.1 は `contract` / `satisfy` / `borrow Dyn<Contract>` を実装対象にする
- generic bounds、owned dynamic object、最適化された vtable layout は後続 phase に分離する
