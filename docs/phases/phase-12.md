# Phase 12: unsafe / C ABI

状態: 完了

## 目的

低レベル操作と C ABI 境界を明示的に扱えるようにする。

## TODO

- [x] `unsafe { ... }` を定義する
- [x] `unsafe fn` を定義する
- [x] `ptr<T>` / `ptr<const T>` を型検査する
- [x] `?ptr<T>` / `?ptr<const T>` を型検査する
- [x] pointer read / write の builtin を決める
- [x] raw pointer と safe borrow を別物として扱う
- [x] raw pointer dereference を unsafe 必須にする
- [x] `extern "c" fn` を parse する
- [x] `extern "c" fn` の呼び出しを unsafe 必須にする
- [x] C ABI の primitive type mapping を定義する
- [x] safe wrapper の例を追加する

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] unsafe block 内で raw pointer operation を呼べる
- [x] unsafe 外の raw pointer operation は error になる
- [x] extern C call は unsafe 必須になる
- [x] unsafe code の memory safety obligation が診断または doc に明示される
- [x] unsafe 内でも moved value の再利用は error になる
- [x] unsafe 内でも borrow escape は error になる
- [x] unsafe 内でも safe borrow の lifetime extension は error になる

## 実装メモ

Phase 12 は C と実際に link する phase ではなく、構文と静的境界を実装する。

採用した構文:

```kizu
extern "c" fn read_byte_raw(p: ptr<const u8>) -> u8

fn read_byte(p: ptr<const u8>) -> u8 {
    unsafe {
        return read_byte_raw(p)
    }
}
```

pointer builtin:

```text
ptr_read(p)          // unsafe required
ptr_write(p, value)  // unsafe required
```

`ptr_write` は `ptr<const T>` と `?ptr<T>` を拒否する。
`extern "c" fn` と `unsafe fn` の呼び出しは unsafe block または unsafe function 内でのみ許可する。

safe wrapper の例は `examples/unsafe_wrapper.kizu` に置く。

## 範囲外

- C header import
- C preprocessor
- 暗黙の integer promotion
- build script
- 明示 lifetime annotation
- actual C linking
- C function execution in the interpreter
