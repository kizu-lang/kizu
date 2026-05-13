# Phase 14: C header import

状態: 完了

## 目的

C header から Kizu の extern 宣言を生成できるようにする。

## TODO

- [x] header import の範囲を決める
- [x] clang など外部 tool への依存方針を決める
- [x] import した宣言の cache key を決める
- [x] `kizu import-c-header <file>` のような CLI を検討する
- [x] unsupported C feature の error 方針を決める

## 受け入れ条件

- [x] `pre-commit run --all-files` が通る
- [x] 小さな C header から extern fn を生成できる
- [x] unsupported syntax が読める error になる

## v0.1 構文

CLI:

```sh
kizu import-c-header <file>
```

入力:

```c
int puts(const char *s);
void write_byte(unsigned char *p, unsigned char value);
```

出力:

```kizu
extern "c" fn puts(s: ptr<const i8>) -> i32
extern "c" fn write_byte(p: ptr<u8>, value: u8) -> void
```

## 実装メモ

- Phase 14 は clang / libclang に依存しない
- C preprocessor は実行しない
- 対応範囲は function prototype のみに限定する
- `const T*` は `ptr<const T>` に変換する
- unnamed parameter は `p1`, `p2` のように生成する
- unsupported syntax は `c import error: ...` として返す
- 現時点では import 結果を build cache に保存しない

将来 cache に統合する場合の cache key:

```text
importer version
header path
header content hash
target ABI
import options
```

## 範囲外

- C preprocessor 完全互換
- C++ header import
- macro import
