# ADR-0020: arena / handle は std::arena の型コンストラクタとして扱う

Status: 採用

## 背景

Kizu は borrowed return の由来を `borrows <source>` で明示する。
一方で、AST や graph の長寿命 identity は borrow ではなく、
`std::arena::Arena<T>` と `std::arena::Handle<T>` で表す。

一方で、v0 では full generics を実装しない。
そのため `std::arena::Arena<T>(allocator)` を通常の generic function body として扱うと、
実装範囲が広がりすぎる。

Phase 6 では、arena / handle の安全性と使い方を先に固定する必要がある。

## 決定

v0 では `std::arena::Arena<T>(allocator)` を compiler-known な stdlib 型コンストラクタとして扱う。
`<T>` は明示 type argument として扱う。
allocator は hidden global ではなく、呼び出し側が明示する capability とする。

採用する構文:

```kizu
let allocator = std::mem::page_allocator();
let users = std::arena::Arena<User>(allocator);
let alice = users.add(User { name: "alice" });
print(users.get(alice).name);
```

意味:

- `std::arena::Arena<T>(allocator)` は `Allocator` capability を明示して `std::arena::Arena<T>` を作る
- allocator 引数は読み取りであり、arena 構築で move されない
- `arena.add(value)` は `value` を arena に move する
- `arena.add(value)` は `std::arena::Handle<T>` を返す
- `arena.get(handle)` は所有権を移さず、ローカル borrow 相当の値を返す
- `arena.deinit()` は明示 cleanup boundary であり、arena binding を無効化する
- `std::arena::Handle<T>` は raw pointer ではなく opaque ID として扱う
- `std::arena::Handle<T>` は対応する arena より長生きしてはいけない
- 別 arena 由来の handle を `get` に渡してはいけない
- `deinit` 後の arena と、その arena 由来の既知 handle は使ってはいけない

## v0 の制約

`std::arena::Arena<T>(allocator)` は generic function body ではない。
parser は通常の namespace-qualified type application + call として読み、
checker / ownership checker / interpreter / IR lowering が arena 固有の安全規則を適用する。
`std::arena::Arena<T>()`、`std::arena::Arena<T>(a, b)`、非 `Allocator` 引数は拒否する。
`arena.deinit()` は 0 引数で、owned local arena receiver だけに許可する。
borrow 中の arena、field receiver、temporary receiver、deinit 後の再利用は拒否する。

v0 では次を実装しない。

- 任意の generic function
- 任意の generic method
- arena からの削除
- generational index
- compacting arena
- concurrent arena
- `std::arena::Handle<T>` から raw pointer を取り出す機能

## provenance

ownership checker は、`arena.add(value)` がどの arena から handle を作ったかを記録する。

`arena.get(handle)` では、handle の provenance が receiver の arena と一致することを検査する。

local arena から作られた handle を関数から返すことは、arena より長生きする可能性があるため拒否する。

## 影響

- Phase 6 は full generics なしで arena / handle を実装できる
- `std::arena::Arena<T>` / `std::arena::Handle<T>` は stdlib の PascalCase owned type naming と一致する
- raw pointer は Phase 12 の unsafe 境界で扱い、`std::arena::Handle<T>` とは別物として扱う
- ADR-0060 の「view return は provenance、graph identity は arena / handle」と整合する
- ADR-0017 の safe Kizu メモリ安全性保証を支える
