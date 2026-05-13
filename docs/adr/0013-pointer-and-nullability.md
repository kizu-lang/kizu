# ADR-0013: raw pointer は ptr<T> / ptr<const T>、nullable pointer は ?ptr<T> にする

Status: 採用

## 背景

Kizu は C ABI や raw pointer を扱う必要がある。
Rust は `*const T` / `*mut T` を持ち、Zig は `*T` / `*const T` / `?*T` を持つ。

Kizu では読みやすさと明示性を優先する。

## 決定

raw pointer 型は次にする。

```text
ptr<T>
ptr<const T>
```

nullable pointer は次にする。

```text
?ptr<T>
?ptr<const T>
```

意味:

```text
ptr<T>          non-null mutable raw pointer
ptr<const T>    non-null const raw pointer
?ptr<T>         nullable mutable raw pointer
?ptr<const T>   nullable const raw pointer
```

## 影響

- pointer dereference は unsafe 操作にする
- `extern "c" fn` の呼び出しは unsafe 必須にする
- C API の null return は `?ptr<T>` で表す
- safe reference / borrow と raw pointer は別物として扱う
