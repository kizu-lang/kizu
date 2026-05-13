# ADR-0020: arena / handle は v0 専用構文として扱う

Status: 採用

## 背景

Kizu は明示 lifetime annotation を採用しない。
長寿命の関係は borrow ではなく、`arena<T>` と `handle<T>` で表す。

一方で、v0 では full generics を実装しない。
そのため `arena<T>()` を通常の generic function call として扱うと、実装範囲が広がりすぎる。

Phase 6 では、arena / handle の安全性と使い方を先に固定する必要がある。

## 決定

v0 では `arena<T>()` を専用の組み込み構文として扱う。

採用する構文:

```kizu
let users = arena<User>()
let alice = users.add(User { name: "alice" })
print(users.get(alice).name)
```

意味:

- `arena<T>()` は `arena<T>` を作る
- `arena.add(value)` は `value` を arena に move する
- `arena.add(value)` は `handle<T>` を返す
- `arena.get(handle)` は所有権を移さず、ローカル borrow 相当の値を返す
- `handle<T>` は raw pointer ではなく opaque ID として扱う
- `handle<T>` は対応する arena より長生きしてはいけない
- 別 arena 由来の handle を `get` に渡してはいけない

## v0 の制約

`arena<T>()` は generic function call ではない。
parser と checker は、v0 専用 construct として扱う。

v0 では次を実装しない。

- 任意の generic function
- 任意の generic method
- arena からの削除
- generational index
- compacting arena
- concurrent arena
- `handle<T>` から raw pointer を取り出す機能

## provenance

ownership checker は、`arena.add(value)` がどの arena から handle を作ったかを記録する。

`arena.get(handle)` では、handle の provenance が receiver の arena と一致することを検査する。

local arena から作られた handle を関数から返すことは、arena より長生きする可能性があるため拒否する。

## 影響

- Phase 6 は full generics なしで arena / handle を実装できる
- `arena<T>` / `handle<T>` は後の generic 実装候補にできるが、v0 では専用扱いを正とする
- raw pointer は Phase 12 の unsafe 境界で扱い、`handle<T>` とは別物として扱う
- ADR-0016 の「明示 lifetime annotation なし」と整合する
- ADR-0017 の safe Kizu メモリ安全性保証を支える
