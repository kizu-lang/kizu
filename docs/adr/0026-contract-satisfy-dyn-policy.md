# ADR-0026: contract / impl / Dyn は明示的な抽象化として扱う

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
contract                型が満たすべき要求
impl Contract for Type  型が contract を満たすことの明示宣言と method body
Dyn                     runtime dynamic dispatch を見せる型
```

## contract

`contract` は required method signatures だけを持つ。
method body は書けない。

```kizu
contract Writer {
    fn write(self: &Self, bytes: &Bytes) -> !i64;
}
```

## impl Contract for Type

`impl Contract for Type` は明示適合と method body を 1 箇所に置く。
Go のような暗黙 interface 適合は採用しない。

```kizu
impl Writer for File {
    fn write(self: &Self, bytes: &Bytes) -> !i64 {
        return os.write(self.fd, bytes);
    }
}
```

`impl Type { ... }` は inherent method 用として残す。contract method body は
`impl Contract for Type { ... }` に置く。旧 `satisfy Contract for Type` 構文は採用しない。

## Dyn

`Dyn<Contract>` は dynamic dispatch を型に見せる。

```kizu
fn save(writer: &Dyn<Writer>, bytes: &Bytes) -> !void {
    let n = writer.write(bytes);
    return void;
}
```

`Dyn<Writer>` と書かれている場所では runtime vtable dispatch が発生してよい。

## 他言語との差分

Rust trait との差分:

- trait system の完全再現をしない
- blanket impl、generic impl、associated type、default method、specialization は持たない
- dynamic dispatch は `Dyn` で明示する

Go interface との差分:

- 暗黙適合を採用しない
- `impl Contract for Type` で適合を明示する

Zig 手書き vtable との差分:

- vtable pattern を手作業だけにしない
- `Dyn<Contract>` で動的ディスパッチの型境界を表す

## 影響

- 抽象化の意図がコード上に残る
- dynamic dispatch が隠れない
- v0.1 は `contract` / `impl Contract for Type` / `&Dyn<Contract>` を実装対象にする
- generic bounds、owned dynamic object、最適化された vtable layout は後続 phase に分離する
