# Kizu 言語仕様 v0.2 prototype

Kizu は、明示的で、シンプルで、メモリ安全なプログラミング言語です。

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
safe Kizu の詳細な安全契約と regression coverage は
[`docs/memory-safety.md`](docs/memory-safety.md) を正とします。

## 0. v0.1 / v0.2 の範囲

Kizu v0.1 は、Go 製 interpreter による language core release とします。

v0.1 で完成させる対象は、parser、type checker、move checker、borrow checker、
ownership model、arena / handle、error union / try、限定的な comptime、
および interpreter で実行できる言語コアです。

LLVM IR backend、WASM / WASI backend、C header import、build cache は experimental として扱います。
これらは将来の compiler work の土台ですが、v0.1 の正ではありません。

v0.1 の完了条件には、self-hosting compiler、native executable generation、
full stdlib、Rust 同等以上の runtime performance guarantee は含めません。
ただし、マルチスレッドと async は Kizu の重要な言語特性として扱い、
v0.1 では safe structured concurrency の仕様、checker ルール、interpreter 上の
最小 runtime model を完成対象に含めます。

Kizu v0.2 は、将来の self-host compiler を進めるための最小 stdlib prototype とします。
v0.2 の正も Go 製 interpreter と `kizu check` です。native executable generation、
package manager、full stdlib、self-host compiler completion は v0.2 の完了条件に含めません。

### 0.1 v0.1 に含めるもの

v0.1 の正は Go 製 interpreter と `kizu check` です。

v0.1 に含める runtime 言語機能:

```text
fn
explicit return
statement semicolon
let / var
assignment
i64
bool
[]const u8
void
arithmetic / comparison
if / else
while
struct
field access
namespace access with ::
simple enum
enum value access
match over simple enum values
tagged union
match payload binding
function call
&T / &mut T borrow parameter
move semantics
arena<T> / handle<T>
!T / error / try
limited comptime
Io capability
Task / TaskGroup
std::task structured API
contract
satisfy
&Dyn<Contract>
```

v0.1 に含める static / policy 機能:

```text
unsafe boundary checks
extern "c" fn declaration checks
raw pointer type spelling
nullable pointer type spelling
explicit cast<T>(value) checker policy
low-level integer type names in checker
f32 / f64 type names in checker
structured task ownership checks
contract satisfaction checks
```

static / policy 機能は、v0.1 interpreter 上で完全な低レベル実行 semantics を約束しません。

### 0.2 v0.2 に含めるもの

v0.2 に含める stdlib / tooling prototype:

```text
std::mem read-only byte helpers
std::array::Array<T>
std::string::String
std::fmt
std::map::Map<[]const u8, V>
std::testing
std::fs
std::path
std::io
std::process
kizu test <file>
self-host compiler skeleton
module/import syntax and manifest groundwork
defer explicit cleanup statement
match wildcard `_` fallback arm
```

v0.2 の stdlib は self-host compiler を進めるための最小 subset です。
general-purpose production stdlib ではありません。

### 0.3 v0.1 / v0.2 に含めないもの

次は v0.1 / v0.2 の完了条件に含めません。

```text
full generics
type alias
complete fixed-width integer runtime semantics
float literals and float runtime arithmetic
overflow / truncation behavior for every numeric cast
raw pointer runtime operations
actual extern C call execution
option<T> runtime helper
full stdlib
kizu lint
native executable generation
self-hosting compiler
async fn / await syntax
OS thread / event loop / networking runtime
Rust 同等以上の runtime performance guarantee
```

### 0.4 v0.1 / v0.2 メモリ安全 release gate

Kizu v0.1 は、safe Kizu のメモリ安全性を release blocker として扱います。

v0.1 では次を必ず守ります。

```text
use-after-move を許さない
double move を許さない
borrow 中の値の move を許さない
borrow escape を許さない
borrow を struct field に保存させない
borrow を task / comptime / unsafe 境界で延命させない
arena.get(handle) は local borrow だけを返す
別 arena の handle 使用を許さない
handle を raw pointer として扱わせない
unsafe 内でも type check / move check / borrow check を全面的に無効化しない
```

v0.1 release 前に、上記の各項目は checker test または `examples/negative/` で検証します。
safe example は `kizu check` と `kizu run` の対象として維持します。

raw pointer operation、C ABI call、unchecked operation は safe Kizu の保証外です。
これらを使う場合、memory safety obligation はプログラマが負います。

allocator primitive、raw pointer runtime operation は v0.1 では完全実装しません。
実装済みの safe guarantee として扱ってはいけません。

## 1. 目標

Kizu は次を目指します。

- GCなしのメモリ安全性
- 単純な所有権
- move semantics
- borrowed view の戻り値は `borrows <source>` で由来を明示する
- borrow はローカル限定
- 書き方の自由度を増やしすぎない
- 標準ライブラリを厚めにする
- CIを速くする
- ビルドキャッシュを肥大化させない
- 依存グラフを抑える

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

`kizu.toml` は宣言的な TOML manifest です。
build script、plugin、条件分岐、実行可能な設定は書けません。

最小形式:

```toml
[package]
name = "app"
version = "0.1.0"

[modules]
root = "src/main.kizu"
paths = ["src"]
```

`package.name` は user module の root namespace になります。
たとえば `name = "app"` の package では、`src/lexer.kizu` を
`app::lexer` として import します。

module path は file path から決まります。

```text
src/main.kizu       -> app
src/lexer.kizu      -> app::lexer
src/parser/mod.kizu -> app::parser
src/parser/ast.kizu -> app::parser::ast
```

## 4. 設計概要

Kizu の値は、基本的に1つの所有者を持ちます。

所有されている値を関数に渡すと、その値は move されます。
move された値を再利用するとコンパイルエラーになります。

Kizu には borrow があります。ローカル borrow は `&T` / `&mut T` で表します。
関数境界を越えて borrowed view を返す場合は `borrows <source>` で由来を明示します。

borrow は次のことができません。

* struct / union の field に保存できない
* `borrows <source>` なしに関数から返せない
* lexical block の外へ escape できない

長生きする関係は、参照ではなく次の型で表します。

```text
std::mem::Box<T>
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
* 依存を抑えやすい
* CLIを作りやすい
* lexer/parser/interpreter/type checker の実験に向いている

## 6. 基本文法

### 6.1 Hello world

```kizu
fn main() -> void {
    print("hello, kizu");
}
```

### 6.2 変数

```kizu
fn main() {
    let name = "alice";
    var count = 0;
    count = count + 1;
}
```

`let` は immutable です。mutable な変数には `var` を使います。

### 6.3 関数

```kizu
fn add(a: i64, b: i64) -> i64 {
    return a + b;
}
```

戻り値の型を省略した場合は `void` を返します。

戻り値を返す場合は `return expr;` を必須にします。
Rust のような末尾式 return は採用しません。
セミコロンの有無で関数の戻り値が変わる仕様も採用しません。
simple statement の終端には `;` を書けますが、次が `}`、EOF、次の statement 開始、
または `match` arm 区切りの場合は省略できます。
ただし `return` は Rust と同じく `return;` / `return expr;` の `;` を必須にします。
comma-separated list は末尾カンマを許容します。

```kizu
fn bad_add(a: i64, b: i64) -> i64 {
    a + b; // error: non-void function must return explicitly
}
```

`void` 関数では `return` を省略できます。
早期 return が必要な場合は `return` を書きます。
`void` は値ではないため、`return void;` は使いません。

```kizu
fn log(message: []const u8) -> void {
    print(message);
}
```

### 6.3.1 defer cleanup statement

`defer <expr-stmt>;` は、現在の lexical block に明示 cleanup 呼び出しを登録します。
function body も block として扱います。

```kizu
fn main() -> !void {
    let allocator = std::mem::page_allocator();
    let values = std::array::Array<i64>(allocator);
    defer values.deinit();

    try values.append(1);
    return;
}
```

登録された cleanup は block を出るときに、登録順の逆順で実行します。
通常の block exit、明示 `return`、`try` などの error return path でも実行します。

v0.2 で許可する形は cleanup method call の expression statement だけです。

```kizu
defer values.deinit();
defer text.deinit();
defer users.deinit();
```

`defer let ...;`、`defer return ...;`、`defer { ... }`、
`defer defer ...;` は構文として扱いません。
deferred expression は `.deinit()` のような `void` cleanup call でなければなりません。
cleanup 対象は自動探索しません。Drop / RAII / implicit destructor はありません。

deferred cleanup は明示 cleanup call と同じ ownership rule で検査します。
登録時点で receiver を参照できる必要があり、block exit で実行する時点でも
receiver が move 済み、deinit 済み、borrow 中なら拒否します。

### 6.4 struct

```kizu
struct User {
    name: []const u8;
    age: i64;
}
```

### 6.5 namespace access

Kizu は型や名前空間に属する item lookup に `::` を使います。

```kizu
let color = Color::Red;
let shape = Shape::Circle(10);
let io = std::io::blocking();
let group = std::task::Group(io);
```

`.` は runtime value の field / method access だけに使います。

```kizu
print(user.name);
let handle = users.add(user);
```

`Color.Red` や `Shape.Circle(10)` のような dot による enum / union lookup は
compile error です。互換構文としては扱いません。

### 6.6 import と visibility

Kizu の user module は明示 import します。

```kizu
import app::lexer;
import app::parser::ast;

pub fn main() -> void {
    let tokens = try lexer::lex("fn main() -> void { return; }");
    return;
}
```

import は top-level にだけ書けます。
wildcard import、relative import、re-export、alias import は v0.2/v0.3 では扱いません。
cyclic import は compile error です。

import した module は最後の segment 名で参照します。

```kizu
import app::compiler::lexer;

let tokens = try lexer::lex(source);
```

`std::...` は標準ライブラリ namespace として import なしで使えます。
user package に `std` という名前は使えません。

name resolution order:

1. local bindings
2. current module top-level declarations
3. imported module names by last segment
4. built-in root namespace `std`
5. error

同じ last segment を持つ import は compile error です。
local declaration が import module name を shadow することも compile error です。

visibility は default private です。

```kizu
pub struct Token {
    pub kind: TokenKind,
    pub start: i64,
    pub end: i64,
}

enum TokenKind {
    Ident,
    Number,
    Eof,
}

pub fn lex(source: []const u8) -> !std::array::Array<Token> {
    return lex_source(source);
}

fn lex_source(source: []const u8) -> !std::array::Array<Token> {
    ...
}
```

ルール:

* top-level declaration は default private
* 外部 module に見せる top-level declaration には `pub` を付ける
* struct field は default private
* 外部 module に見せる field には `pub` を付ける
* public API に private type を出してはいけない
* 外部 module から private field を construct / access してはいけない
* `pub` な enum の tag と `pub` な union の variant は外部から使える
* `pub(crate)`、`pub(super)`、`protected` は v0.2/v0.3 では採用しない

### 6.7 enum

Kizu の `enum` は Zig/C 寄りの tag enum です。
Rust の payload enum / algebraic data type とは分けます。

v0.1 の enum は、payload を持たない named tag だけを実装します。

```kizu
enum Color {
    Red,
    Green,
    Blue,
}
```

値は `Color::Red` のように enum 型名で修飾して参照します。

```kizu
let color = Color::Red;
```

payload を持つ sum type は `enum` では扱いません。
`union` として別機能で扱います。

### 6.8 union

Kizu の `union` は payload を持てる tagged union です。
tag だけの値が必要な場合は `enum` を使います。

```kizu
union Shape {
    Point,
    Circle(i64),
    Label([]const u8),
}
```

payload を持つ variant は `Shape::Circle(10)` のように構築します。
payload を持たない variant は `Shape::Point` のように参照します。

```kizu
let a = Shape::Circle(10);
let b = Shape::Point;
```

`match` では payload binding を書けます。

```kizu
match a {
    Point => print("point"),
    Circle(radius) => print(radius),
    Label(text) => print(text),
}
```

v0.1 の `union` は次に限定します。

* variant ごとの payload は0個または1個
* pattern guard はない
* destructuring は payload binding 1つだけ
* `match` は exhaustive でなければならない

### 6.9 if

Kizu の `if` は statement と expression の両方で使えます。

```kizu
if age >= 20 {
    print("adult");
} else {
    print("minor");
}
```

expression として使う場合は `else` が必須で、両 branch の末尾 value type が一致しなければ
なりません。関数の戻り値は引き続き明示的な `return` で返します。

```kizu
let label = if age >= 20 {
    "adult"
} else {
    "minor"
}
```

三項演算子は採用しません。

### 6.9.1 bool 演算

Kizu は boolean logic に `and` と `or` を使います。
両辺は `bool` でなければなりません。
`and` と `or` は短絡評価します。

優先順位は低い順に次の通りです。

```text
or
and
== !=
< <= > >=
```

例:

```kizu
if age >= 20 and age < 130 or admin {
    print("ok");
}
```

### 6.10 while

```kizu
while i < 10 {
    print(i);
    i = i + 1;
}
```

Kizu v0.1 は `loop` keyword を採用しません。
無限 loop は `while true` と書きます。

```kizu
while true {
    break;
}
```

`break` と `continue` は loop 内だけで使えます。
外側の loop を明示する場合は Zig 寄りの label を使います。

```kizu
outer: while i < 10 {
    while j < 10 {
        break :outer;
    }
}
```

### 6.11 for

v0.1 の `for` は、i64 の half-open range に限定します。
終了値は含みません。

```kizu
for 0..3 |i| {
    print(i);
}
```

v0.1 では iterator protocol、collection iteration、`inline for` は扱いません。

### 6.12 match

`match` は、単純な enum value と tagged union value を分岐する用途に限定します。
statement と expression の両方で使えます。

```kizu
fn main() {
    let color = Color::Red;

    match color {
        Red => print("red"),
        Green => print("green"),
        Blue => print("blue"),
    }
}
```

guard と多段 destructuring は v0.1 では扱いません。
tagged union の payload binding だけを扱います。
duplicate arm、unknown tag、non-exhaustive match は compile error です。
v0.2 では wildcard pattern `_` を fallback arm として許可します。
`_` arm は最後に 1 つだけ書けます。payload binding はできません。
`_` arm がある場合、明示されていない残りの tag を束ねるため exhaustive とみなします。
`_` arm がない場合は、すべての tag を明示しなければなりません。
expression として使う場合は、すべての arm の value type が一致しなければなりません。

## 7. 型

v0.1 の基本型:

```text
bool
void
```

`i64` は整数 literal のデフォルト型です。
Kizu は `int` のような幅が曖昧な整数型を導入しません。

文字列 literal の型は `[]const u8` です。
`string` primitive は導入しません。

`void` は値を返さない関数の戻り値です。
Kizu v0.1 では `Unit` という別名は導入しません。

低レベル型として、次の明示幅整数、浮動小数点、raw pointer 型名を予約します。
v0.1 では主に checker / unsafe / extern declaration のために扱います。
interpreter 上の完全な fixed-width arithmetic、float literal、overflow semantics は後続 phase で扱います。

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

type alias は v0.1 では導入しません。

v0.1 では collection runtime API を実装しません。
将来追加する collection 型は primitive ではなく、標準ライブラリ型として扱います。

```text
std::array::Array<T>
std::map::Map<K, V>
std::set::Set<T>
```

v0.1 では `arena<T>` / `handle<T>` だけを実装対象にします。
将来追加する ownership/container 型:

```text
std::mem::Box<T>
shared<T>
slice<T>
```

v0.1 では full generics を実装しません。
`arena<T>`、`handle<T>`、`!T`、raw pointer 型は専用の型構文として扱います。

### 7.1 index / slice expression

Kizu は checked index と checked slice syntax を持ちます。

```kizu
let byte = bytes[index];
let part = bytes[start..end];
let tail = bytes[start..];
let head = bytes[..end];
```

v0.2 の最初の対象は `[]const u8` です。

```text
[]const u8 [ i64 ] -> u8
[]const u8 [ i64 .. i64 ] -> []const u8
```

index / slice syntax は 1 次元 contiguous sequence に限定します。
`matrix[rows, cols]` のような multi-dimensional slicing、strided view、
matrix view は言語構文として採用しません。
多次元データは、将来の `std::matrix` などの標準ライブラリ型で明示 API として扱います。

slice bounds は half-open です。
`start..end` は `start` を含み、`end` を含みません。

safe Kizu では unchecked bounds access を許しません。
負の index、負の bound、`start > end`、`end > len` は safety check failure として trap します。
index / slice syntax は recoverable error を返しません。
境界外を回復可能な値として扱いたい場合は、`std::mem::byte_at` や
`std::mem::slice` のような明示 API を使います。

v0.2 では mutable indexed assignment、indexed borrow、multi-dimensional slicing、
`std::array::Array<T>` への直接 indexing は後続に分離します。

### 7.2 明示 cast

Kizu は暗黙の numeric promotion をしません。
異なる numeric type の間で値を渡す場合は、明示的に `cast<T>(value)` を使います。
ただし整数 literal だけは、期待型が明確な文脈では、その整数型として扱えます。

文脈型が効くのは、関数引数、戻り値、既に型が決まっている代入先、struct literal field、
union payload、標準ライブラリの typed container API など、期待する整数型が一意に決まる場所です。
literal の値は対象型の範囲に収まらなければなりません。
範囲外の literal は type error です。
`let x = 1;` のように期待型がない局所 binding では、`x` は従来どおり `i64` です。
一度 `i64` として束縛された値を `u8` / `i32` などへ渡す場合は `cast<T>(x)` が必要です。

```kizu
fn take(x: i32) -> i32 {
    return x;
}

fn main() {
    print(take(1));

    let x = 1;
    print(take(cast<i32>(x)));
}
```

safe code で許可する cast:

```text
numeric type -> numeric type
```

safe code で許可しない cast:

```text
[]const u8 -> numeric
bool -> numeric
numeric -> pointer
pointer -> numeric
pointer -> pointer
```

raw pointer 間の cast は `unsafe` 内でのみ許可します。

```kizu
fn write_as_mut(p: ptr<const u8>) {
    unsafe {
        let q = cast<ptr<u8>>(p);
        ptr_write(q, 1);
    }
}
```

pointer cast の memory safety obligation はプログラマが負います。
ただし、`unsafe` 内でも type check / move check / borrow check は無効化されません。

type alias は v0.1 では導入しません。
必要になった場合は、別 phase で syntax と ABI 上の扱いを決めます。

## 8. 所有権

所有される値を代入または関数引数として渡すと move されます。

v0.1 の copy 型:

```text
bool
void
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
```

copy できない型:

```text
array
map
std::mem::Box<T>
arena-owned value
non-copy field を含む struct
```

`array`、`map`、`std::mem::Box<T>` は v0.1 では実装しません。
将来追加する場合も、copy できない所有値として扱います。

## 9. borrow

borrow は一時的に値を参照するための仕組みです。

```kizu
fn show(s: &[]const u8) {
    print(s);
}
```

mutable borrow には `&mut T` を使います。

```kizu
fn update(user: &mut User) -> void {
    user.*.name = "bob";
}
```

borrow のルール:

* borrow は一時的
* local borrow binding は straight-line code では最後に使った場所で終了する
* borrow argument は呼び出し statement の終了で終了する
* borrow field は v0.2 では struct / union に保存できない
* borrow return は `borrows <source>` を必須にする
* borrow 中の値は move できない
* `&T` と `&mut T` は重複できない
* `&mut T` 同士は同じ値に対して重複できない
* `&mut T` argument は mutable local binding に限定する
* v0.1 は `&user.name` のような one-level direct field borrow を許可する
* field borrow 中でも disjoint field assignment は許可する
* field borrow 中の owner 全体の move と同一 field assignment は禁止する
* v0.1 は `&user.profile.name` のような nested field borrow を拒否する
* v0.2 の index / slice expression は read-only checked access から始める
* indexed borrow syntax はまだ実装しない。将来 `&items[0]` を追加する場合は、
  専用の安全ルールと regression coverage を先に追加する

境界に現れる borrowed-return provenance syntax:

```kizu
fn first(bytes: []const u8) -> []const u8 borrows bytes
fn show(value: &i64) -> &i64 borrows value
```

`borrows source` は戻り値が `source` 引数または `self` receiver 由来の view であり、
その source より長生きできないことを表します。名前付き lifetime parameter、
`&'a T`、`[]'a const T`、lifetime bounds、anonymous lifetime は採用しません。
borrow field や複数 source 由来の戻り値は、後続の bounded issue で必要性を確認します。

明示 dereference は Zig に合わせて postfix の `.*` を使います。

```kizu
fn rename(user: &mut User) -> void {
    user.*.name = "bob";
}
```

local borrow binding:

```kizu
fn main() -> void {
    let name = "alice";
    let r = &name;
    print(r.*);
    print(name); // ok: r は最後の使用後に終了している
}
```

field borrow:

```kizu
struct User {
    name: []const u8;
    age: i64;
}

fn main() -> void {
    var user = User { name: "alice", age: 30 };
    let name = &user.name;
    user.age = 31; // ok: name と age は disjoint field
    print(name.*);
}
```

assignment のルール:

* `let` binding への再代入は禁止
* `let` binding の field assignment は禁止
* `var` binding の field assignment は許可
* `&T` 経由の dereference assignment は禁止
* `&mut T` 経由の dereference assignment は許可

```kizu
fn main() -> void {
    var user = User { name: "alice" };
    user.name = "bob";
}
```

## 10. arena / handle

Kizu は、長寿命の参照を複雑な lifetime で表さず、`arena<T>` と `handle<T>` で表します。

```kizu
let allocator = std::mem::page_allocator();
let users = arena<User>(allocator);
let alice = users.add(User { name: "alice" });
print(users.get(alice).name);
```

`arena<T>` は複数の `T` を所有します。
v0 core arena の構築は明示 allocator capability を要求し、
`arena<T>()` は無効です。allocator 引数は読み取りとして扱われ、move されません。

`handle<T>` はポインタではありません。arena 内の値を指す opaque な ID です。

ルール:

* `arena<T>(allocator)` は `Allocator` を明示して `arena<T>` を作る
* `arena<T>.add(value)` は value を arena に move する
* `arena<T>.add(value)` は `handle<T>` を返す
* `arena<T>.get(handle)` はローカル borrow を返す
* `arena<T>.deinit()` は arena を明示 cleanup し、binding を無効化する
* `arena<T>.deinit()` は owned local receiver の 0 引数呼び出しだけを許可する
* handle は borrow より長生きしてよい
* handle は対応する arena より長生きしてはいけない
* `deinit` 後の arena と、その arena 由来の既知 handle は使用してはいけない
* handle は raw pointer ではない
* v0.1 では arena からの削除は実装しない

## 11. エラー処理

Kizu は exception を使いません。
error は値として扱います。

v0.1 では Zig に近い `!T` を導入します。
`!T` は「成功時は `T`、失敗時は error」を表します。
error payload は標準の `[]const u8` message として扱います。
domain 固有の custom error を型として扱いたい場合は、v0.1 では `union` と
`match` を使います。
typed error として伝播したい場合は `ErrorType!T` を使います。

成功時は通常の `T` をそのまま `return` します。
error 値は `error(message)` で作ります。

```kizu
fn parse() -> !i64 {
    return 1;
}

fn fail() -> !i64 {
    return error("bad");
}
```

`try` は `!T` を unwrap します。
error の場合は、現在の関数からその error value を返します。

```kizu
fn main() -> !i64 {
    let value = try parse();
    return value + 1;
}
```

custom error type を明示的に扱う例:

```kizu
union ConfigError {
    NotFound([]const u8),
    InvalidPort(i64),
}

union ConfigRead {
    Ok(i64),
    Err(ConfigError),
}

fn main() -> void {
    let result = ConfigRead::Err(ConfigError::NotFound("config.kizu"));

    match result {
        Ok(port) => print(port),
        Err(err) => match err {
            NotFound(path) => print(path),
            InvalidPort(port) => print(port),
        },
    }
}
```

typed error を `try` で伝播する例:

```kizu
union ConfigError {
    NotFound([]const u8);
    InvalidPort(i64);
}

fn read_port(ok: bool) -> ConfigError!i64 {
    if ok {
        return 8080;
    }

    return ConfigError::NotFound("config.kizu");
}

fn main() -> ConfigError!void {
    let port = try read_port(true);
    print(port);
    return;
}
```

ルール:

* `try` は `!T` を返す関数内でだけ使える
* `try` の operand は `!T` でなければならない
* `ErrorType!T` は typed error union を表す
* `ErrorType!T` では `ErrorType` または `T` を返せる
* `try` は同じ `ErrorType` の error union だけを伝播できる
* `cast<ErrorType!T>(expr)` は `expr: !T` を明示的に typed error union へ変換できる
* typed error cast は `ErrorType::Message([]const u8)` variant がある場合だけ有効で、
  untyped error message をその variant に包む
* `!T` 関数では `T` を返すと成功値として扱う
* `error(message)` は `!T` を返す関数内でだけ使える
* `error(message)` は typed error union では使えない
* `error(message)` の message は `[]const u8`
* `error(message)` は message bytes を error payload に copy して所有する
* `error(message)` は borrow view を保持しないため、local `String.as_bytes()` view から
  diagnostic を作れる
* `!void` の成功 return は `return;` と書く
* exception / stack unwinding は使わない
* `option<T>` は型名として予約するが、v0.1 では runtime helper を実装しない

## 12. unsafe / C ABI

`unsafe` は、コンパイラが memory safety を証明しない操作を明示する境界です。

```kizu
unsafe {
    ptr_write(p, 20);
}
```

unsafe function も明示します。

```kizu
unsafe fn raw_write(p: ptr<u8>, value: u8) -> void {
    ptr_write(p, value);
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

### 12.1 C header import

Kizu は C header の完全互換 parser は持ちません。
Phase 14 の header import は、限定された C function prototype を `extern "c" fn` に変換する補助機能です。
これは v0.1 の正ではなく experimental です。

CLI:

```sh
kizu import-c-header <file>
```

例:

```c
int puts(const char *s);
void write_byte(unsigned char *p, unsigned char value);
```

出力:

```kizu
extern "c" fn puts(s: ptr<const i8>) -> i32
extern "c" fn write_byte(p: ptr<u8>, value: u8) -> void
```

Phase 14 で対応するもの:

* function prototype
* `void`
* `char` / `signed char` / `unsigned char`
* `short` / `int` / `long long` と unsigned variant
* `int*_t` / `uint*_t`
* `size_t` / `intptr_t`
* `float` / `double`
* 単純な pointer
* `const T*` から `ptr<const T>` への変換

Phase 14 で対応しないもの:

* C preprocessor 完全互換
* macro import
* typedef
* struct / enum / union declaration
* variadic function
* function pointer
* array parameter
* C++ header

header import は build script ではありません。
外部 tool には依存せず、入力 header text から deterministic に extern 宣言を出力します。
将来 build cache に統合する場合の cache key は、importer version、header path、header content hash、
target ABI、import option を含めます。

### 12.2 C ABI layout / linking 方針

Kizu の通常 `struct` は C layout を約束しません。
C ABI と共有する layout は、将来 `extern struct` または `repr(c)` 相当で明示します。

検討する構文:

```kizu
extern struct Point {
    x: i32
    y: i32
}
```

または:

```kizu
@repr("c")
struct Point {
    x: i32
    y: i32
}
```

link name / library 指定も暗黙にしません。
将来必要になった場合は attribute として扱います。

```kizu
@link_name("puts")
@link_lib("c")
extern "c" fn c_puts(s: ptr<const u8>) -> i32
```

compiler runtime が使う symbol は `kizu_` prefix を予約します。

```text
kizu_print_string
kizu_print_int
kizu_print_bool
```

LLVM IR backend では、extern C call は将来 `declare` と `call` に lower します。
native executable generation は、LLVM lowering 済み subset と `kizu_print_*` runtime shim に
限定して扱います。extern C library selection と C layout 完全対応は別 phase で扱います。

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

v0.1 の最小構文:

```kizu
let size = comptime 4 * 1024;
```

comptime parameter:

```kizu
fn sized(comptime n: i64) -> i64 {
    return n;
}
```

Std source may define restricted generic wrappers when the type
argument is forwarded to an explicit trusted primitive:

```kizu
pub fn Channel<T>() -> Channel<T> {
    return std::builtin::channel<T>();
}

pub fn Array<T>(allocator: Allocator) -> std::array::Array<T> {
    return std::builtin::array<T>(allocator);
}

pub fn Atomic<T>(value: T) -> Atomic<T> {
    return std::builtin::atomic<T>(value);
}

pub fn Mutex<T>(value: T) -> Mutex<T> {
    return std::builtin::mutex<T>(value);
}

pub fn Map<K, V>(allocator: Allocator) -> std::map::Map<K, V> {
    return std::builtin::map<K, V>(allocator);
}
```

This is not a general monomorphization system. v0.2 uses it only to move public
stdlib constructor spelling into Kizu source while keeping runtime storage as an
explicit primitive boundary.

comptime branch:

```kizu
comptime if 1 + 1 == 2 {
    print(sized(comptime 8));
} else {
    print(0);
}
```

v0.1 の `comptime` expression は、整数、真偽値、文字列、単項演算、二項演算だけを評価します。
runtime local value は `comptime` expression から参照できません。

`comptime Function` parameter is a restricted function-name token for std
wrappers that must forward a named function to a trusted primitive. It is not a
closure, cannot capture locals, and cannot be stored in runtime data. A
`Function` parameter must be marked `comptime`, and the argument must be a
top-level function name.

`comptime if` は、コンパイル時に選ばれた branch だけを検査し、lowering します。
これは token stream や AST を書き換える macro ではありません。

## 14. 標準ライブラリ方針

Kizu は将来的に厚めの標準ライブラリを持ちます。

v0.1 の最小 builtin は `print` です。

```text
print
```

加えて、v0.1 は concurrency / async の安全境界を固めるために、
`std::task`、`std::channel`、`std::thread`、`std::atomic`、`std::sync`
の prototype API を interpreter builtin として持ちます。
これらは full stdlib ではなく、memory-safety release gate の対象となる
trusted std prototype です。

stdlib module は lowercase namespace names にします。

```text
std::string
std::io
std::fs
std::mem
std::slice
std::array
std::map
```

文字列 literal は v0.1 では `[]const u8` として扱います。
owned string は primitive ではなく、将来 `std::string::String` で扱います。

C ABI へ `std::string::String` を暗黙に渡してはいけません。
C へ渡す場合は、将来 `std::string::as_c_string` のような明示 API を使います。

v0.2 で安定化する allocator factory は `std::mem::page_allocator()` です。

```text
std::mem::page_allocator() -> Allocator
```

`Allocator` は visible opaque capability type です。
v0.2 では user-facing `contract` ではなく、field を持つ struct でもなく、
user code が実装できる interface でもありません。
safe Kizu code は `Allocator` 型を名前として使い、local binding に束縛し、
明示 allocator を要求する API に渡せます。

`Allocator` は copy 型です。`Array<T>`、`String`、`Map<K, V>`、`Box<T>`、
`arena<T>` の構築に渡しても allocator binding は move されません。
作られた owner は自身の allocation と `deinit` に必要な runtime allocator handle を
保持しますが、`Allocator` 値そのものに user-visible cleanup method はありません。
allocation が失敗し得る API は `!T` または `!void` で失敗を返します。

hidden default allocator、implicit global allocator、missing allocator argument から
`page_allocator()` への fallback は使いません。
safe `std::mem` は raw pointer、allocation method、mutable backing slice、
allocator metadata、deallocation primitive を公開しません。
user-defined allocator、fixed-buffer allocator、testing allocator は #549 で別途仕様化します。

v0.2 の `std::string::String` は、明示 allocator capability を受け取る
owned byte buffer です。

```text
std::string::String(allocator: Allocator) -> std::string::String
string.append_bytes(bytes: []const u8) -> !void
string.append_byte(byte: u8) -> !void
string.reserve(additional: i64) -> !void
string.truncate(length: i64) -> !void
string.len() -> i64
string.capacity() -> i64
string.as_bytes() -> []const u8
string.clear() -> void
string.deinit() -> void
```

`string` primitive は追加しません。
`std::string::String()` のような hidden default allocator は使いません。
`std::string::String` は non-copy / move-only です。
`append_bytes` は source の `[]const u8` を move せず、owned buffer に copy します。
`append_byte` は 1 byte を追加します。
`reserve` は少なくとも `additional` byte 分の追加 capacity を確保し、失敗時は `!void` を返します。
`truncate` は length を短くし、capacity は保持します。範囲外の length は `!void` error です。
`capacity` は現在の capacity を `i64` で返します。
`as_bytes` は owned buffer への local read-only view です。
`as_bytes` の戻り値は local binding に束縛する必要があります。
view が生きている間は `append_bytes`、`append_byte`、`truncate`、`clear`、`deinit` を禁止します。
`append_bytes`、`append_byte`、`reserve`、`truncate`、`clear` は owned local `String` または
`&mut std::string::String` から呼べます。
`clear` は length を 0 にしますが、capacity は保持します。
`deinit` は caller 側の binding を無効化する必要があるため、owned local receiver 限定です。
v0.2 では UTF-8 validation、C ABI string 変換、raw pointer exposure、
owned bytes 取り出し、String 専用 comparison、String 専用 indexing / slicing は実装しません。
`std::string::String` の public behavior は `std/src/string.kizu` に実装します。
v0.2 では private `std::array::Array<u8>` storage の上に構成し、safe Kizu に
raw pointer や mutable backing slice は公開しません。public
`std::mem::OwnedBytes` または `std::bytes::Buffer` は、mutable slice と raw
storage provenance の仕様後に検討します。

v0.2 の `std::fmt` は、diagnostic construction 用の最小 formatting API です。
format string、locale、generic display trait、reflection は持ちません。
caller が明示 allocator 付きの `std::string::String` を用意し、
formatting API はその buffer に bytes を append します。

```text
std::fmt::append_i64(out: &mut std::string::String, value: i64) -> !void
std::fmt::append_bool(out: &mut std::string::String, value: bool) -> !void
std::fmt::append_bytes_literal(
    out: &mut std::string::String,
    bytes: []const u8,
) -> !void
```

hidden global allocator は使いません。
allocation failure は `String` の allocator から `!void` error として伝播します。
output は conformance test 向けに deterministic ASCII とします。
`append_i64` は 10 進表記で、負数には `-` を付け、`+` と不要な leading zero は出しません。
`append_bool` は `true` または `false` を出します。
`append_bytes_literal` は diagnostic 用の quoted byte string を出します。
printable ASCII byte のうち `"` と `\` 以外はそのまま出します。
`"`、`\`、newline、carriage return、tab はそれぞれ `\"`、`\\`、`\n`、`\r`、`\t`
として escape します。
その他の byte は uppercase hex の `\xNN` として escape します。

collection は次の順で実装します。

```text
std::array::Array<T>  先に検討する owned contiguous collection
slice<T>              contiguous mutable view
slice<const T>        contiguous read-only view
std::map::Map<K, V>   self-host compiler 向け symbol table
std::set::Set<T>      後続 phase
```

v0.2 の `std::mem` は、self-host compiler の lexer が必要とする
allocation-free な read-only byte helper から始めます。

```text
std::mem::page_allocator() -> Allocator
std::mem::Box<T>(allocator: Allocator, value: T) -> !std::mem::Box<T>
box.borrow() -> &T borrows self
box.borrow_mut() -> &mut T borrows self
box.deinit() -> void
std::mem::len(bytes: []const u8) -> i64
std::mem::byte_at(bytes: []const u8, index: i64) -> !u8
std::mem::equal_bytes(left: []const u8, right: []const u8) -> bool
std::mem::starts_with(bytes: []const u8, prefix: []const u8) -> bool
std::mem::slice(bytes: []const u8, start: i64, end: i64) -> ![]const u8 borrows bytes
std::mem::trim_ascii(bytes: []const u8) -> []const u8 borrows bytes
```

`std::mem::page_allocator()` は v0.2 の安定 allocator capability factory です。
返された `Allocator` は copy 型であり、複数の owned container や arena の構築に
再利用できます。allocator を受け取る constructor は capability を読み取るだけで、
allocator binding を move しません。

`std::mem::Box<T>` は明示 allocator capability で 1 つの owned value を確保する
non-copy / move-only な indirection です。`Box<T>` は struct / union payload に保存できます。
`Box<T>` を含む struct / union は non-copy です。
`borrow` / `borrow_mut` は local borrow source であり、戻り値は local binding に束縛する
必要があります。borrow return は `borrows self` のように source が結び付く場合だけ
許可します。borrow field は v0.2 では許可しません。borrow が生きている間は対象
`Box<T>` の move / deinit を禁止します。
`deinit` は owned local `Box<T>` receiver 限定です。
safe API は raw pointer を公開しません。

`std::mem` の safe API は raw pointer を返しません。
`std::mem::slice` と `std::mem::byte_at` は境界外アクセスを `!T` として返します。
checked index / slice syntax の実装後は、Kizu std source では
trap-on-bounds-failure の syntax と recoverable な `std::mem` API を用途で使い分けます。
allocator、mutable slice、byte copy / zero / fill は、`std::array::Array<T>` と
mutable slice の仕様後に実装します。

v0.2 の `std::array::Array<T>` は、明示 allocator capability を受け取る
owned contiguous collection です。

```text
std::mem::page_allocator() -> Allocator
std::array::Array<T>(allocator: Allocator) -> std::array::Array<T>
array.append(value: T) -> !void
array.len() -> i64
array.capacity() -> i64
array.get(index: i64) -> !T
array.get_or_panic(index: i64) -> T
array.at(index: i64) -> !&T borrows self
array.at_mut(index: i64) -> !&mut T borrows self
array.set(index: i64, value: T) -> !void
array.deinit() -> void
```

`std::array::Array<T>()` のような hidden default allocator は使いません。
`array.get` は bounds check し、範囲外なら `!T` の error を返します。
`array.get_or_panic` は testing や invariant-checked code 用の明示 trap variant です。
範囲外なら runtime error で停止するため、recoverable lookup には `get` を使います。
v0.2 の `get` / `get_or_panic` は copy element 限定です。
non-copy element は `at` / `at_mut` で local borrow として読み書きします。
element borrow が生きている間は `append`、`set`、`deinit` を禁止します。
mutable element borrow が生きている間は array 全体の read も禁止します。
`deinit` 後の array 使用は safe Kizu では禁止します。
v0.2 の `Array<T>` element には raw pointer、arena、handle、nested array、
`std::map::Map<K, V>`、concurrency capability type を入れられません。
この制限は struct field と union payload の中も再帰的に検査します。
これらは lifetime、provenance、thread boundary
の仕様を collection 向けに固めてから扱います。

v0.2 の `std::map::Map<K, V>` は、self-host compiler の symbol table と
scope lookup に必要な最小 owned map です。

```text
std::map::Map<[]const u8, V>(allocator: Allocator) -> std::map::Map<[]const u8, V>
map.insert(key: []const u8, value: V) -> !void
map.get(key: []const u8) -> !V
map.contains(key: []const u8) -> bool
map.len() -> i64
map.deinit() -> void
```

v0.2 では key type は `[]const u8` 限定です。
`insert` は key bytes を owned map 内に copy するため、source key を move しません。
`get` は missing key を `!V` の error として返します。
v0.2 の value type は copy type 限定です。
non-copy value、borrow view、iteration、deletion、custom hash/equality は後続で扱います。
`std::map::Map<K, V>()` のような hidden default allocator は使いません。
`deinit` 後の map 使用は safe Kizu では禁止します。
`std::map::Map<K, V>` は v0.2 では task/thread/channel boundary を越えられません。

v0.2 の `std::testing` は、self-host compiler component test 用の
最小 assertion API です。

```text
std::testing::expect(condition: bool) -> void
std::testing::fail(message: []const u8) -> !void
```

`expect` は test assertion 用の void helper です。
condition failure は `std::builtin::test_fail` 経由で runtime error として停止し、
test source は assertion ごとの `try` を書きません。
`fail` は caller-provided `[]const u8` を通常の `!void` error として返します。
unreachable branch など、呼び出し側の error-union 経路へ明示的に戻したい場合に使います。
generic equality は v0.2 では導入せず、比較は `expect(left == right)` または
`expect(std::mem::equal_bytes(left, right))` のように caller 側で明示します。
typed `expect_equal_<type>` family は v0.2 API に含めません。
`kizu test <file>` は v0.2 では discovery なしの single-file runner です。
file を check して `main` を実行し、未処理 error がなければ `test: ok` を表示します。
test discovery、location-aware diagnostics、message builder helper は後続で扱います。

## 15. concurrency / async 方針

Kizu v0.1 では `async fn` / `await` syntax は実装しません。
ただし、非同期・マルチスレッド周りの標準ライブラリ API 形状と
safe Kizu の安全契約は v0.1 で固定します。

ただし、I/O と並行処理の境界は v0.1 から実装対象にします。
I/O は `Io` capability として明示し、並行処理は `Task` / `TaskGroup` で明示します。
Kizu は Zig 0.16 寄りに、hidden global runtime ではなく、明示的な `Io`
interface を渡す設計にします。

v0.1 で固定するのは API と checker rule です。
実 OS thread、event loop、networking runtime、advanced atomic ordering は実装しません。
interpreter は同期実行でもよいですが、将来の実並行 runtime でも同じ API と
ownership / borrow rule を維持できる必要があります。

```kizu
fn read_config(io: Io, path: []const u8) -> ![]const u8 {
    return std::fs::read_file(io, path);
}
```

方針:

* I/O する関数は `Io` を受け取る
* `Io` implementation は将来 `std::io` で明示的に選ぶ
* hidden global async runtime は持たない
* `TaskGroup` で structured concurrency に寄せる
* detached task は許可しない
* spawn された task は await または cancel される必要がある
* task は local borrow を捕まえられない
* task へ渡す non-copy value は move される
* 野良 task は許可しない
* task は `TaskGroup` の structured scope を越えて escape できない
* safe Kizu では task 間で mutable state を暗黙共有できない
* channel に送れる値は owned value または copy value に限定する
* data parallelism は `std::task::parallel_for` のような structured API に閉じ込める
* shared mutable state は `std::sync` / `std::atomic` の明示型だけで扱う

v0.1 の `TaskGroup` は interpreter 上の structured task model として実装します。
`std::io::blocking()` と `std::io::failing()` は同期評価します。
`std::io::threaded()` は `group.spawn` を goroutine で実行し、`await` / `cancel` が
完了を待ちます。

v0.1 で追加していく concurrency foundation:

```text
std::task::Group          structured task scope
Task<T>                   awaited or canceled task result
std::task::Queue          deterministic deferred task queue
std::task::parallel_for   safe data parallelism
std::task::parallel_map   disjoint partition output
std::channel::Channel<T>  owned message passing
std::thread::scoped<T>    scoped thread boundary
std::sync::Mutex<T>       explicit shared mutable state wrapper
std::atomic::Atomic<T>   seq_cst-only atomic primitive
Io                        explicit I/O capability
std::fs::read_file        explicit-Io file read returning ![]const u8
std::fs::write_file       explicit-Io file write returning !void
std::path                 pure path helpers with no hidden I/O
std::process              explicit process argv/env helpers
```

v0.1 の `std::io` implementation:

```text
std::io::blocking()  simple blocking I/O
std::io::threaded()  thread-backed I/O and task execution
std::io::failing()   test implementation that supports no external I/O
```

将来の `std::io` implementation 候補:

```text
std::io::evented()   event-loop or coroutine backed I/O
std::io::uring()     Linux io_uring backend
std::io::kqueue()    kqueue backend
```

v0.1 では `evented` / `uring` / `kqueue` は実装しません。
ただし、safe Kizu の checker rule はどの runtime implementation でも同じです。

v0.1 では次の API 形状を正とします。

```kizu
let io = std::io::blocking();

let group = std::task::Group(io);
let task = group.spawn(load_config, "config.kizu");
let value = try task.await();

let ch = std::channel::Channel<i64>();
ch.send(1);
let n = ch.recv();

let result = std::thread::scoped<i64>(io, worker, 41);
let lock = std::sync::Mutex<i64>(3);
let atomic = std::atomic::Atomic<i64>(0);
```

`Task<T>`:

* `std::task::Group(io)` は task group を `Io` implementation に紐づける
* `group.spawn(fn, args...)` は `Task<T>` を返す
* spawn 対象関数は第1引数に `Io` を受け取る
* `T` は spawn 対象関数の戻り値
* `task.await()` は `T` または `!T` を返す
* `task.cancel()` は `void` を返す
* task は scope を抜ける前に await または cancel されなければならない
* `await()` は task body の error を呼び出し側へ伝播する
* `cancel()` は v0.1 では cooperative cancellation ではない
* `cancel()` は task の完了を待ち、結果または error を破棄する
* `await()` 後の `cancel()` と `cancel()` 後の `await()` はエラー
* `threaded` runtime の `cancel()` は v0.1 では実行中 task の完了を待って結果を破棄する

`std::fs`:

* `std::fs::read_file(io, path)` は `![]const u8` を返す
* `std::fs::write_file(io, path, bytes)` は `!void` を返す
* `std::fs::exists(io, path)` は `!bool` を返す
* `std::fs::metadata(io, path)` は `!std::fs::Metadata` を返す
* `std::fs::read_dir(io, path)` は `!std::array::Array<std::fs::DirEntry>` を返す
* `std::fs::create_dir(io, path)` は `!void` を返す
* `std::fs::remove_dir(io, path)` は `!void` を返す
* `std::fs::remove_file(io, path)` は `!void` を返す
* `std::fs::Metadata` は v0.2 では `size: i64` と `is_dir: bool` だけを持つ
* `std::fs::DirEntry` は v0.2 では `name: []const u8`、`path: []const u8`、`is_dir: bool` だけを持つ
* `path` と `bytes` は caller 側の `[]const u8` を保持しない read-only borrow
* I/O failure は `!T` error として返す
* hidden global runtime や暗黙 blocking I/O は使わない
* `std::io::failing()` は deterministic failing I/O として、テストで I/O error path を確認する

`std::path`:

* `std::path::join(allocator, left, right)` は `!std::string::String` を返す
* `std::path::clean(allocator, path)` は `!std::string::String` を返す
* `std::path::basename(path: []const u8) -> []const u8 borrows path`
* `std::path::dirname(path: []const u8) -> []const u8 borrows path`
* `std::path::extension(path: []const u8) -> []const u8 borrows path`
* path helper は pure helper であり、filesystem を読まない
* `join` と `clean` は owned buffer を構築するため、allocator を明示し、allocation
  failure を `!T` error として返す

`std::io` / `std::process`:

* `std::io::write_stdout(io, bytes)` は `!void` を返す
* `std::io::write_stderr(io, bytes)` は `!void` を返す
* `std::io::read_stdin(io)` は `![]const u8` を返す
* stdio helper は `Io` capability を必ず要求する
* `std::process::arg_count()` は `i64` を返す
* `std::process::arg(index)` は `![]const u8` を返す
* `std::process::env(name)` は `![]const u8` を返す
* `std::process::exit_code(code)` は `i64` を返す
* `std::process` helper は hidden I/O を持たない

`std::channel::Channel<T>` is owned message passing:

* `send(value)` moves non-copy values into the channel
* `send(value)` requires `value: T`
* `recv()` returns an owned `T`
* borrow values and raw pointers cannot cross the channel boundary in safe Kizu
* v0.1 では blocking semantics は定義しない
* empty `recv()` は runtime error
* v0.1 does not include `select`

`std::task::parallel_for` is safe data parallelism:

* v0.1 workers are `fn(i: i64) -> void` or `fn(i: i64) -> !void`
* `std::task::partition_mut(init: i64, count: i64)` creates disjoint `i64` output slots
* `partition.at(i)` reads or writes one checked slot
* `std::task::parallel_map(io, partition, start, end, worker)` takes `partition`
  as `&mut Partition` and writes `worker(i)` to slot `i`
* `std::task::LocalBuffer` is the trusted boundary for worker-local scratch
* first error propagation uses the existing `!void` / `try` model
* the interpreter may execute workers sequentially while preserving the API contract
* v0.1 の `parallel_for` は range 専用で、collection iteration には接続しない
* v0.1 の `parallel_map` output は `Partition` に限定する
* mutable slice / array との接続は `std::mem` と `std::array::Array<T>` の仕様後に設計する
* 詳細は ADR-0040 に従う

Low-level concurrency boundary:

* `std::thread::scoped<T>` is scoped and joined by construction
* v0.2 の `std::thread::scoped<T>(io, fn, arg)` は 1 引数 worker に限定し、
  `fn(arg)` の結果を返す
* v0.1 interpreter では OS thread を作らず同期評価してよい
* `std::sync::Mutex<T>` は explicit shared-mutable-state wrapper
* v0.1 の `Mutex<T>` は copy value だけを受け取る
* guard / lock mutation semantics と non-copy payload は後続で固める
* `std::atomic::Atomic<T>` は v0.1 seq_cst-only。v0.1 の T は `bool` と `i64`
* v0.1 では atomic ordering parameter を持たない
* raw pointers cannot cross thread/task/channel/mutex boundaries in safe Kizu

Send 相当ルール:

* Rust の `Send` trait は採用しない
* v0.1 では concurrency boundary を越えられる型を checker rule として明示する
* copy primitive、enum、safe field だけを持つ owned struct / union は boundary を越えられる
* `Atomic<T>` は `T` が v0.1 atomic 対応型なら boundary を越えられる
* `Channel<T>` は `T` が boundary-safe な場合だけ boundary を越えられる
* local borrow、mutable borrow、raw pointer は safe Kizu では boundary を越えられない
* raw pointer を field / payload に含む struct / union も boundary を越えられない
* `arena<T>` / `handle<T>` / `Dyn<Contract>` / `Mutex<T>` / `Task<T>` は
  v0.1 では boundary を越えられない
* arena / handle の thread-safe sharing は v0.1 では扱わない

OS thread、event loop、networking runtime、atomic ordering の詳細 API は、
safe structured API の後に追加します。
実並行 runtime を導入する場合も、上記の ownership / borrow / structured scope の制約を維持します。
詳細な runtime selection 方針は ADR-0039 に従います。

## 16. contract / satisfy / Dyn 方針

Kizu v0.1 では、Rust trait clone ではない明示的な抽象化として、
`contract`、`satisfy`、`Dyn` を実装対象にします。

```text
contract  型が満たすべき要求
satisfy   型が contract を満たすことの明示宣言
Dyn       runtime dynamic dispatch を見せる型
```

`contract` は required method signatures だけを書きます。
method body は書けません。

```kizu
contract Writer {
    fn write(self: &Self, bytes: &Bytes) -> !i64
}
```

`satisfy` は明示適合だけを表します。
Go のような暗黙 interface 適合は採用しません。

```kizu
satisfy Writer for File
```

method body は型のそばに置きます。

```kizu
impl File {
    fn write(self: &File, bytes: &Bytes) -> !i64 {
        return os.write(self.fd, bytes);
    }
}
```

`Dyn<Contract>` は dynamic dispatch を型に見せます。

```kizu
fn save(writer: &Dyn<Writer>, bytes: &Bytes) -> !void {
    let n = writer.write(bytes);
    return;
}
```

v0.1 の `Dyn` は `&Dyn<Contract>` の動的 dispatch に限定します。
owned dynamic object、generic bounds、最適化された vtable layout は後続 phase で扱います。

## 17. ビルドとキャッシュ

Kizu の toolchain は、キャッシュが無制限に肥大化しない設計にします。

v0.1 / v0.2 の正として扱うコマンド:

```text
kizu parse
kizu check
kizu run
kizu fmt
kizu test
```

`kizu fmt` は現時点では compact AST formatter output です。
完全な source-preserving formatter ではありません。
`kizu test` は v0.2 では discovery なしの single-file runner です。

experimental tooling:

```text
kizu build
kizu ir
kizu cache status
kizu cache prune
kizu why-rebuild
kizu import-c-header
```

生成物は `target/` 配下で output family ごとに分けます。

```text
target/
  check/
  interp/
  ir/
  native/
  wasm/
  c/
  cache/
```

`kizu check` は durable artifact をデフォルトでは生成しません。
IR、WASM、native、C 出力は明示的な build command でだけ生成します。

native build は Zig を参考に、target、ABI、libc mode、runtime mode、linker mode を
明示的な build input として扱います。現時点の native backend は host `clang` と libc を
使ってよいですが、将来の `--libc off` / freestanding build を一級の build mode として
扱います。libc 依存は言語仕様に埋め込まず、build metadata と cache key に含めます。
native build は生成物の隣に `<output>.kizu-build.json` を書き、実際に使った target、
ABI、libc、runtime、emit mode、linker command を記録します。

build cache key には、compiler version、manifest hash、resolved module graph hash、
source hash、public interface hash、target、backend、optimization mode、stdlib hash
を含めます。

`kizu test` は v0.2 では discovery なしの single-file runner として実装済みです。
`kizu lint` は未実装です。

## 18. v0.1 実装構成

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

## 19. 実装マイルストーン

### Milestone 1: Lexer

対応する token:

```text
identifier
integer literal
[]const u8 literal
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
defer cleanup statement
if statement
if expression
while statement
function call
binary expression
struct declaration
struct literal
field access
namespace access
enum declaration
union declaration
match statement
match expression
borrow expression
arena type and constructor
error union type
comptime expression and statement
contract / satisfy / dyn type
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
enum and union values
match
arena / handle
error union / try
limited comptime
std task / channel / thread prototypes
defer cleanup statement
```

### Milestone 4: Type checker

未定義変数、型不一致、不正な二項演算、不正な field access を検査します。

### Milestone 5: Move checker

use after move、double move、function argument への move、assignment による move を検査します。

### Milestone 6: Local borrow checker

borrow escape、borrow 中の move、mutable borrow conflict を検査します。

### Milestone 7: arena / handle

`arena<T>(allocator)`、`arena.add(value)`、`arena.get(handle)`、`arena.deinit()` を
runtime-level で実装します。

### Milestone 8: typed SSA IR

checked AST から typed SSA IR に lowering します。
これは v0.1 の正ではなく experimental tooling です。

### Milestone 9: LLVM IR backend

typed SSA IR から LLVM IR を生成します。
LLVM lowering は interpreter より限定された subset だけを扱います。
これは v0.1 の正ではありません。

### Milestone 10: build cache / why-rebuild

キャッシュ状態、キャッシュ削除、再ビルド理由を確認できるようにします。
build cache は compiler work のための experimental tooling です。

### Milestone 11: WASM / WASI backend

typed SSA IR から WASM を生成し、WASI で実行できるようにします。
WASM target は interpreter より限定された subset だけを扱います。
これは v0.1 の正ではありません。

### Milestone 12: unsafe / C ABI

`unsafe`、raw pointer、`extern "c" fn` を扱えるようにします。

### Milestone 13: comptime

限定的な `comptime` を実装します。
macro / proc macro / AST rewrite は実装しません。

### Milestone 14: C header import

C header から Kizu の extern 宣言を生成できるようにします。
これは v0.1 の正ではなく experimental tooling です。

## 20. 最初に通す examples

最初に `examples/hello.kizu` を通します。

```kizu
fn main() -> void {
    print("hello, kizu");
}
```

## 21. エラーメッセージ方針

エラーは短く、直接的で、読めるものにします。

良い例:

```text
error: moved value `name` was used
  --> examples/move_error.kizu:8:11
```

## 22. 言語の性格

Kizu は次のような言語にします。

* 仕様を絞る
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
