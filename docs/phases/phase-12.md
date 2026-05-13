# Phase 12: unsafe / C ABI

状態: 未着手

## 目的

低レベル操作と C ABI 境界を明示的に扱えるようにする。

## TODO

- [ ] `unsafe { ... }` を定義する
- [ ] `unsafe fn` を定義する
- [ ] `ptr<T>` / `ptr<const T>` を型検査する
- [ ] `?ptr<T>` / `?ptr<const T>` を型検査する
- [ ] pointer read / write の builtin を決める
- [ ] raw pointer と safe borrow を別物として扱う
- [ ] raw pointer dereference を unsafe 必須にする
- [ ] `extern "c" fn` を parse する
- [ ] `extern "c" fn` の呼び出しを unsafe 必須にする
- [ ] C ABI の primitive type mapping を定義する
- [ ] safe wrapper の例を追加する

## 受け入れ条件

- [ ] `pre-commit run --all-files` が通る
- [ ] unsafe block 内で raw pointer operation を呼べる
- [ ] unsafe 外の raw pointer operation は error になる
- [ ] extern C call は unsafe 必須になる
- [ ] unsafe code の memory safety obligation が診断または doc に明示される
- [ ] unsafe 内でも moved value の再利用は error になる
- [ ] unsafe 内でも borrow escape は error になる
- [ ] unsafe 内でも safe borrow の lifetime extension は error になる

## 範囲外

- C header import
- C preprocessor
- 暗黙の integer promotion
- build script
- 明示 lifetime annotation
