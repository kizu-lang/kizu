# Phase XX: contract / satisfy / Dyn policy

状態: 完了

## 目的

Rust trait clone ではない抽象化として、`contract` / `satisfy` / `Dyn` の方針を仕様化する。

v0 では実装せず、将来の v1/v2 候補として管理する。

## 方針

- `contract` は要求だけを書く
- `satisfy` は型が contract を満たすことの明示宣言にする
- method body は `impl Type` に置く
- `Dyn<Contract>` は runtime dynamic dispatch を型に見せる
- Go のような暗黙 interface 適合は採用しない
- Rust trait system の完全再現は目指さない
- v0 では parser / checker 実装をしない

## TODO

- [x] `docs/phases/phase-xx-contract.md` を整理する
- [x] `contract` は要求だけを書く方針を明記する
- [x] `satisfy` は明示適合だけにする方針を明記する
- [x] `Dyn<Contract>` で dynamic dispatch を見える化する方針を明記する
- [x] method は型のそばに置く方針を確認する
- [x] v0 では実装しないことを明記する

## 受け入れ条件

- [x] contract system の方針が `SPEC.md` または ADR にある
- [x] Rust trait / Go interface / Zig 手書き vtable との差分が説明されている
- [x] v0 に入れない理由が明確

## 構文案

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> !i64
}
```

```kizu
impl File {
    fn write(self: borrow File, bytes: borrow Bytes) -> !i64 {
        return os.write(self.fd, bytes)
    }
}

satisfy Writer for File
```

```kizu
fn save(writer: borrow Dyn<Writer>, bytes: borrow Bytes) -> !void {
    let n = writer.write(bytes)
    return void
}
```

## 導入順

```text
v0: contract なし
v1: impl Type による method system
v2: contract / satisfy
v3: generic bound
v4: borrow Dyn<Contract>
v5: owned dynamic object を検討
```

## 範囲外

- contract parser 実装
- generic bounds 実装
- vtable layout 実装
- operator overloading
