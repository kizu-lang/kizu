# ADR-0038: namespace lookup は `::` に限定する

Status: 採用

## 背景

Kizu は Zig 寄りの明示的な低レベル言語を目指す。
enum tag、union variant、標準ライブラリの名前空間、値の field / method access を
すべて `.` で表すと、型空間の lookup と runtime value access が同じ見た目になる。

Kizu では構文の混在を避け、読み手が式の意味を見た目で区別できることを優先する。

## 決定

型または名前空間に属する item lookup は `::` を使う。

```kizu
let color = Color::Red;
let shape = Shape::Circle(10);
let io = std::io::blocking();
let bytes = try std::fs::read_file(io, "config.toml");
```

runtime value の field / method access は `.` を使う。

```kizu
print(user.name);
let handle = users.add(user);
let value = users.get(handle).name;
```

`Color.Red` や `Shape.Circle(10)` のような enum / union の dot lookup は
compile error とする。互換構文として残さない。

## 影響

- enum tag と union variant は `Type::Item` で統一される
- 標準ライブラリ API は `std::module::item` 形式へ寄せる
- `.` は値の field / method access だけを表す
- parser / checker / interpreter は `::` と `.` を別ノード属性として扱う
- 旧 dot 構文の fallback は置かない
