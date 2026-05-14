# ADR-0035: v0.1 loop control は while / for / labeled branch に限定する

Status: 採用

## 背景

Kizu v0.1 には、システムプログラミング言語として最低限の loop control が必要です。
Kizu は Zig 寄りの低レベル言語を目指すため、Rust の `loop` keyword や iterator-heavy
な `for` ではなく、明示的で小さい構文を優先します。

## 決定

v0.1 の loop control は次に限定します。

- `while condition { ... }`
- `while true { ... }` を無限 loop の標準形にする
- `break`
- `continue`
- `label: while ...` / `label: for ...`
- `break :label`
- `continue :label`
- `for start..end |i| { ... }`

`for` は i64 の half-open range だけを扱います。終了値は含みません。

```kizu
for 0..3 |i| {
    print(i)
}
```

外側の loop を明示して抜ける場合は Zig 寄りの labeled branch を使います。

```kizu
outer: while i < 10 {
    while j < 10 {
        break :outer
    }
}
```

## 非採用

- `loop {}` keyword
- `while else`
- `while` continue expression
- full iterator protocol
- collection iteration
- `inline for`
- loop expression value

## 影響

v0.1 の control flow は小さく、interpreter と将来の self-host compiler で再実装しやすい。
`for` は将来 std collection が整った後に拡張できます。
