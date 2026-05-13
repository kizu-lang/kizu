# ADR-0018: 戻り値は explicit return にする

Status: 採用

## 背景

Rust では block の末尾式が戻り値になる。
また、semicolon の有無で式の値が変わる。

Kizu は読みやすさと明快さを優先する。
制御フローや戻り値が、semicolon の有無や式文脈に隠れることは避けたい。

## 決定

戻り値を返す場合は `return` を必須にする。

Rust 風の tail expression return は採用しない。
セミコロンの有無で戻り値が変わる仕様も採用しない。

```kizu
fn add(a: int, b: int) -> int {
    return a + b
}
```

次は error にする。

```kizu
fn add(a: int, b: int) -> int {
    a + b
}
```

`void` 関数では `return` を省略できる。
早期 return が必要な場合は `return` を書く。

```kizu
fn log(message: string) -> void {
    print(message)
}
```

```kizu
fn maybe_log(enabled: bool) -> void {
    if !enabled {
        return
    }

    print("enabled")
}
```

## 影響

- parser は tail expression return を特別扱いしない
- type checker は non-void 関数に return path があることを検査する
- semicolon は戻り値の意味を変えない
- Kizu は Rust より冗長だが、戻り値が明示的になる
