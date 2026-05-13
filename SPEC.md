# Kizu 言語仕様 v0.1

Kizu は、小さく、シンプルで、メモリ安全なプログラミング言語です。

名前の Kizu は日本語の「傷」に由来します。

Kizu の思想は次の通りです。

> 傷を作らない。傷を隠さない。

Kizu は Rust のようなメモリ安全性の考え方を参考にしますが、Rust 互換を目指しません。

Rust より単純で、C/C++/Zig より安全で、CI とビルドキャッシュが重くなりにくい言語を目指します。

Kizu はシステムプログラミング言語を目指します。
型名や数値型の設計は Zig に近い、低レベル寄りの明示性を優先します。

Kizu のメモリ安全性保証は safe Kizu に対して行います。
`unsafe` を使うコードでは、memory safety obligation はプログラマが負います。
ただし、`unsafe` は型検査、move check、borrow check を全面的に無効化するものではありません。

## 1. 目標

Kizu は次を目指します。

- GCなしのメモリ安全性
- 単純な所有権
- move semantics
- 明示的な lifetime 注釈なし
- borrow はローカル限定
- 書き方の自由度を増やしすぎない
- 標準ライブラリを厚めにする
- CIを速くする
- ビルドキャッシュを肥大化させない
- 依存グラフを小さく保つ

## 2. 非目標

Kizu は次を目指しません。

- Rust 互換
- C++ 互換
- proc macro
- macro
- build script
- 複雑な lifetime programming
- 高度な trait system
- C++ テンプレートのようなメタプログラミング
- 何通りもの書き方を許す表現力
- 低レベルポインタ操作をすべて静的に安全証明すること

## 3. ファイル規約

ソースファイル拡張子:

```text
.kizu
```

CLI名:

```text
kizu
```

マニフェストファイル:

```text
kizu.toml
```

ロックファイル:

```text
kizu.lock
```

## 4. 設計概要

Kizu の値は、基本的に1つの所有者を持ちます。

所有されている値を関数に渡すと、その値は move されます。
move された値を再利用するとコンパイルエラーになります。

Kizu には borrow がありますが、borrow はローカル限定です。
Rust のような明示 lifetime annotation は採用しません。

borrow は次のことができません。

* struct の field に保存できない
* 関数から返せない
* lexical block の外へ escape できない

長生きする関係は、参照ではなく次の型で表します。

```text
box<T>
shared<T>
arena<T>
handle<T>
```

## 5. v0 の実装方針

最初の実装は、ネイティブコンパイラではなくインタプリタにします。

実装言語:

```text
Go
```

理由:

* 実装が速い
* 依存を小さく保ちやすい
* CLIを作りやすい
* lexer/parser/interpreter/type checker の実験に向いている

## 6. 基本文法

### 6.1 Hello world

```kizu
fn main() {
    print("hello, kizu")
}
```

### 6.2 変数

```kizu
fn main() {
    let name = "alice"
    var count = 0
    count = count + 1
}
```

`let` は immutable です。mutable な変数には `var` を使います。

### 6.3 関数

```kizu
fn add(a: int, b: int) -> int {
    return a + b
}
```

戻り値の型を省略した場合は `void` を返します。

戻り値を返す場合は `return` を必須にします。
Rust のような末尾式 return は採用しません。
セミコロンの有無で戻り値が変わる仕様も採用しません。

```kizu
fn bad_add(a: int, b: int) -> int {
    a + b // error: non-void function must return explicitly
}
```

`void` 関数では `return` を省略できます。
早期 return が必要な場合は `return` を書きます。

```kizu
fn log(message: string) -> void {
    print(message)
}
```

### 6.4 struct

```kizu
struct User {
    name: string
    age: int
}
```

### 6.5 enum

v0 の enum は、まず単純なタグ付き値だけでよいです。

```kizu
enum Color {
    Red
    Green
    Blue
}
```

将来的には payload を持つ enum を追加します。

### 6.6 if

```kizu
if age >= 20 {
    print("adult")
} else {
    print("minor")
}
```

### 6.7 while

```kizu
while i < 10 {
    print(i)
    i = i + 1
}
```

### 6.8 match

`match` は将来追加します。v0 では optional です。

## 7. 型

v0 の基本型:

```text
int
bool
string
void
```

`int` は v0 の簡易整数型です。
interpreter 上では符号付き整数として扱い、具体的な bit 幅は固定しません。

低レベル型として、次の明示幅整数と raw pointer 型を持ちます。

```text
i8
i16
i32
i64
u8
u16
u32
u64
usize
isize
f32
f64
ptr<T>
ptr<const T>
?ptr<T>
?ptr<const T>
```

将来追加する collection 型:

```text
array<T>
map<K, V>
set<T>
```

将来追加する ownership/container 型:

```text
box<T>
shared<T>
arena<T>
handle<T>
borrow<T>
slice<T>
```

v0 では、generics は構文だけ先に予約してもよいですが、完全実装は不要です。

## 8. 所有権

所有される値を代入または関数引数として渡すと move されます。

v0 の copy 型:

```text
int
bool
void
```

copy できない型:

```text
string
array
map
box
arena-owned value
non-copy field を含む struct
```

## 9. borrow

borrow は一時的に値を参照するための仕組みです。

```kizu
fn show(s: borrow string) {
    print(s)
}
```

borrow のルール:

* borrow は一時的
* borrow は struct に保存できない
* borrow は関数から返せない
* borrow 中の値は move できない
* mutable borrow と immutable borrow は重複できない
* v0 では mutable borrow は後回しでもよい

## 10. arena / handle

Kizu は、長寿命の参照を複雑な lifetime で表さず、`arena<T>` と `handle<T>` で表します。

```kizu
let users = arena<User>()
let alice = users.add(User { name: "alice" })
print(users.get(alice).name)
```

`arena<T>` は複数の `T` を所有します。

`handle<T>` はポインタではありません。arena 内の値を指す opaque な ID です。

ルール:

* `arena<T>.add(value)` は value を arena に move する
* `arena<T>.add(value)` は `handle<T>` を返す
* `arena<T>.get(handle)` はローカル borrow を返す
* handle は borrow より長生きしてよい
* handle は対応する arena より長生きしてはいけない
* handle は raw pointer ではない
* v0 では arena からの削除は実装しなくてよい

## 11. エラー処理

Kizu は将来的に `Option<T>` と `Result<T, E>` を持ちます。

v0 では interpreter error だけでもよいです。

## 12. unsafe / C ABI

`unsafe` は、コンパイラが memory safety を証明しない操作を明示する境界です。

```kizu
unsafe {
    ptr_write(p, 20)
}
```

unsafe function も明示します。

```kizu
unsafe fn raw_write(p: ptr<u8>, value: u8) -> void {
    ptr_write(p, value)
}
```

C ABI declaration は `extern "c" fn` で書きます。

```kizu
extern "c" fn puts(s: ptr<const u8>) -> i32
```

ルール:

* raw pointer operation は `unsafe` 内でのみ使える
* `extern "c" fn` の呼び出しは `unsafe` 内でのみ行える
* `ptr<T>` は non-null mutable raw pointer
* `ptr<const T>` は non-null const raw pointer
* `?ptr<T>` / `?ptr<const T>` は nullable raw pointer
* safe borrow と raw pointer は別物として扱う
* `ptr_read(p)` は `ptr<T>` / `ptr<const T>` から `T` を読む
* `ptr_write(p, value)` は `ptr<T>` に `T` を書く
* `ptr_write` は `ptr<const T>` と nullable pointer には使えない

unsafe code の memory safety obligation はプログラマが負います。
ただし、`unsafe` は compiler check を全面的に無効化するものではありません。

`unsafe` 内でも次は error のままです。

* type mismatch
* moved value の safe use
* borrow escape
* safe borrow の lifetime extension

C ABI primitive type mapping:

```text
i8/u8      signed char / unsigned char 相当
i16/u16    int16_t / uint16_t 相当
i32/u32    int32_t / uint32_t 相当
i64/u64    int64_t / uint64_t 相当
usize      size_t 相当
isize      intptr_t 相当
ptr<T>     T*
ptr<const T> const T*
?ptr<T>    nullable T*
```

## 13. comptime

`comptime` は、限定的なコンパイル時評価です。

Kizu の `comptime` は macro ではありません。
コンパイル時に評価される、型検査済みの Kizu コードとして扱います。

方針:

* `comptime` は型検査と ownership / borrow check の対象にする
* AST や token stream を直接書き換える macro API は提供しない
* runtime borrow が comptime 境界を越えて escape することは禁止する
* comptime の結果は型付きの値または宣言として扱う
* build script として使える任意の副作用は許さない

検討する構文:
v0.1 の最小構文:

```kizu
let size = comptime 4 * 1024
```

comptime parameter:

```kizu
fn sized(comptime n: int) -> int {
    return n
}
```

comptime branch:

```kizu
comptime if 1 + 1 == 2 {
    print(sized(comptime 8))
} else {
    print(0)
}
```

v0.1 の `comptime` expression は、整数、真偽値、文字列、単項演算、二項演算だけを評価します。
runtime local value は `comptime` expression から参照できません。

`comptime if` は、コンパイル時に選ばれた branch だけを検査し、lowering します。
これは token stream や AST を書き換える macro ではありません。

## 14. 標準ライブラリ方針

Kizu は将来的に厚めの標準ライブラリを持ちます。

v0 で必要なのはこれだけです。

```text
print
```

## 15. ビルドとキャッシュ

Kizu の toolchain は、キャッシュが無制限に肥大化しない設計にします。

将来のコマンド:

```text
kizu build
kizu run
kizu test
kizu fmt
kizu lint
kizu cache status
kizu cache prune
kizu why-rebuild
```

## 16. v0 実装構成

推奨リポジトリ構成:

```text
kizu/
  README.md
  SPEC.md
  AGENTS.md
  go.mod
  cmd/
    kizu/
      main.go
  internal/
    token/
    lexer/
    ast/
    parser/
    interp/
    types/
    ownership/
  examples/
    hello.kizu
  tests/
```

## 17. 実装マイルストーン

### Milestone 1: Lexer

対応する token:

```text
identifier
integer literal
string literal
keyword
operator
punctuation
```

### Milestone 2: Parser

parse するもの:

```text
function declaration
block
let declaration
var declaration
assignment
return statement
if statement
while statement
function call
binary expression
struct declaration
struct literal
field access
```

### Milestone 3: Interpreter

実行できるもの:

```text
print("hello")
integer arithmetic
boolean comparison
variables
functions
if
while
struct value
field access
```

### Milestone 4: Type checker

未定義変数、型不一致、不正な二項演算、不正な field access を検査します。

### Milestone 5: Move checker

use after move、double move、function argument への move、assignment による move を検査します。

### Milestone 6: Local borrow checker

borrow escape、borrow 中の move、mutable borrow conflict を検査します。

### Milestone 7: arena / handle

`arena<T>()`、`arena.add(value)`、`arena.get(handle)` を runtime-level で実装します。

### Milestone 8: typed SSA IR

checked AST から typed SSA IR に lowering します。

### Milestone 9: LLVM IR backend

typed SSA IR から LLVM IR を生成します。

### Milestone 10: build cache / why-rebuild

キャッシュ状態、キャッシュ削除、再ビルド理由を確認できるようにします。

### Milestone 11: WASM / WASI backend

typed SSA IR から WASM を生成し、WASI で実行できるようにします。

### Milestone 12: unsafe / C ABI

`unsafe`、raw pointer、`extern "c" fn` を扱えるようにします。

### Milestone 13: comptime

限定的な `comptime` を実装します。
macro / proc macro / AST rewrite は実装しません。

### Milestone 14: C header import

C header から Kizu の extern 宣言を生成できるようにします。

## 18. 最初に通す examples

最初に `examples/hello.kizu` を通します。

```kizu
fn main() {
    print("hello, kizu")
}
```

## 19. エラーメッセージ方針

エラーは短く、直接的で、読めるものにします。

良い例:

```text
error: moved value `name` was used
  --> examples/move_error.kizu:8:11
```

## 20. 言語の性格

Kizu は次のような言語にします。

* 小さい
* 直接的
* 厳格
* 実用的
* 読みやすい
* 儀式が少ない
* clever すぎない
* macro-heavy ではない
* 魔法に頼らない

標語:

```text
Kizu: 傷を作らない。傷を隠さない。
```

英語標語:

```text
Kizu: no hidden damage.
```
