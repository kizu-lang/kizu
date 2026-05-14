# Kizu 言語仕様 v0.1

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

## 0. v0.1 の範囲

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

### 0.2 v0.1 に含めないもの

次は v0.1 の完了条件に含めません。

```text
full generics
type alias
complete fixed-width integer runtime semantics
float literals and float runtime arithmetic
overflow / truncation behavior for every numeric cast
raw pointer runtime operations
actual extern C call execution
array / map / set / slice runtime API
option<T> runtime helper
full stdlib
kizu test
kizu lint
native executable generation
self-hosting compiler
async fn / await syntax
OS thread / event loop / networking runtime
Rust 同等以上の runtime performance guarantee
```

### 0.3 v0.1 メモリ安全 release gate

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
- 明示的な lifetime 注釈なし
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
* 依存を抑えやすい
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

戻り値を返す場合は `return` を必須にします。
Rust のような末尾式 return は採用しません。
セミコロンの有無で戻り値が変わる仕様も採用しません。
simple statement の終端には `;` を必須にします。

```kizu
fn bad_add(a: i64, b: i64) -> i64 {
    a + b; // error: non-void function must return explicitly
}
```

`void` 関数では `return` を省略できます。
早期 return が必要な場合は `return` を書きます。

```kizu
fn log(message: []const u8) -> void {
    print(message);
}
```

### 6.4 struct

```kizu
struct User {
    name: []const u8
    age: i64
}
```

### 6.5 namespace access

Kizu は型や名前空間に属する item lookup に `::` を使います。

```kizu
let color = Color::Red;
let shape = Shape::Circle(10);
let group = std::task::Group();
```

`.` は runtime value の field / method access だけに使います。

```kizu
print(user.name);
let handle = users.add(user);
```

`Color.Red` や `Shape.Circle(10)` のような dot による enum / union lookup は
compile error です。互換構文としては扱いません。

### 6.6 enum

Kizu の `enum` は Zig/C 寄りの tag enum です。
Rust の payload enum / algebraic data type とは分けます。

v0.1 の enum は、payload を持たない named tag だけを実装します。

```kizu
enum Color {
    Red
    Green
    Blue
}
```

値は `Color::Red` のように enum 型名で修飾して参照します。

```kizu
let color = Color::Red
```

payload を持つ sum type は `enum` では扱いません。
`union` として別機能で扱います。

### 6.7 union

Kizu の `union` は payload を持てる tagged union です。
tag だけの値が必要な場合は `enum` を使います。

```kizu
union Shape {
    Point
    Circle(i64)
    Label([]const u8)
}
```

payload を持つ variant は `Shape::Circle(10)` のように構築します。
payload を持たない variant は `Shape::Point` のように参照します。

```kizu
let a = Shape::Circle(10)
let b = Shape::Point
```

`match` では payload binding を書けます。

```kizu
match a {
    Point => print("point")
    Circle(radius) => print(radius)
    Label(text) => print(text)
}
```

v0.1 の `union` は次に限定します。

* variant ごとの payload は0個または1個
* pattern guard はない
* destructuring は payload binding 1つだけ
* `match` は exhaustive でなければならない

### 6.8 if

Kizu v0.1 の `if` は statement と expression の両方で使えます。

```kizu
if age >= 20 {
    print("adult");
} else {
    print("minor");
}
```

expression 位置の `if` は `else` が必須です。
各 branch block の最後の expression statement が branch value になります。

```kizu
let level = if age >= 20 {
    1;
} else {
    0;
};
```

両 branch の value type は一致しなければなりません。
branch 内で move された値は、`if` expression の外側でも moved として扱います。

### 6.9 while

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
        break :outer
    }
}
```

### 6.10 for

v0.1 の `for` は、i64 の half-open range に限定します。
終了値は含みません。

```kizu
for 0..3 |i| {
    print(i)
}
```

v0.1 では iterator protocol、collection iteration、`inline for` は扱いません。

### 6.11 match

v0.1 の `match` は、単純な enum value と tagged union value を分岐する用途に限定します。

```kizu
fn main() {
    let color = Color::Red

    match color {
        Red => print("red")
        Green => print("green")
        Blue => print("blue")
    }
}
```

guard と多段 destructuring は v0.1 では扱いません。
tagged union の payload binding だけを扱います。
duplicate arm、unknown tag、non-exhaustive match は compile error です。
wildcard pattern `_` は v0.1 では採用しません。

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
将来追加する collection 型:

```text
array<T>
map<K, V>
set<T>
```

v0.1 では `arena<T>` / `handle<T>` だけを実装対象にします。
将来追加する ownership/container 型:

```text
box<T>
shared<T>
slice<T>
```

v0.1 では full generics を実装しません。
`arena<T>`、`handle<T>`、`!T`、raw pointer 型は専用の型構文として扱います。

### 7.1 明示 cast

Kizu は暗黙の numeric promotion をしません。
異なる numeric type の間で値を渡す場合は、明示的に `cast<T>(value)` を使います。

```kizu
fn take(x: i32) -> i32 {
    return x
}

fn main() {
    let x = cast<i32>(1)
    print(take(x))
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
        let q = cast<ptr<u8>>(p)
        ptr_write(q, 1)
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
box
arena-owned value
non-copy field を含む struct
```

`array`、`map`、`box` は v0.1 では実装しません。
将来追加する場合も、copy できない所有値として扱います。

## 9. borrow

borrow は一時的に値を参照するための仕組みです。

```kizu
fn show(s: &[]const u8) {
    print(s)
}
```

mutable borrow には `&mut T` を使います。

```kizu
fn update(user: &mut User) -> void {
    user.*.name = "bob"
}
```

borrow のルール:

* borrow は一時的
* borrow は struct に保存できない
* borrow は関数から返せない
* borrow 中の値は move できない
* `&T` と `&mut T` は重複できない
* `&mut T` 同士は同じ値に対して重複できない
* `&mut T` argument は mutable local binding に限定する

明示 dereference は Zig に合わせて postfix の `.*` を使います。

```kizu
fn rename(user: &mut User) -> void {
    user.*.name = "bob"
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
    var user = User { name: "alice" }
    user.name = "bob"
}
```

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
    return 1
}

fn fail() -> !i64 {
    return error("bad")
}
```

`try` は `!T` を unwrap します。
error の場合は、現在の関数からその error value を返します。

```kizu
fn main() -> !i64 {
    let value = try parse()
    return value + 1
}
```

custom error type を明示的に扱う例:

```kizu
union ConfigError {
    NotFound([]const u8);
    InvalidPort(i64);
}

union ConfigRead {
    Ok(i64);
    Err(ConfigError);
}

fn main() -> void {
    let result = ConfigRead::Err(ConfigError::NotFound("config.kizu"));

    match result {
        Ok(port) => print(port);
        Err(err) => match err {
            NotFound(path) => print(path);
            InvalidPort(port) => print(port);
        }
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
    return void;
}
```

ルール:

* `try` は `!T` を返す関数内でだけ使える
* `try` の operand は `!T` でなければならない
* `ErrorType!T` は typed error union を表す
* `ErrorType!T` では `ErrorType` または `T` を返せる
* `try` は同じ `ErrorType` の error union だけを伝播できる
* `!T` 関数では `T` を返すと成功値として扱う
* `error(message)` は `!T` を返す関数内でだけ使える
* `error(message)` は typed error union では使えない
* `error(message)` の message は `[]const u8`
* exception / stack unwinding は使わない
* `option<T>` は型名として予約するが、v0.1 では runtime helper を実装しない

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
actual native object / executable generation は別 phase で扱います。

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
let size = comptime 4 * 1024
```

comptime parameter:

```kizu
fn sized(comptime n: i64) -> i64 {
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

stdlib module は lowercase namespace names にします。

```text
std::string
std::io
std::fs
std::mem
std::slice
std::array
```

文字列 literal は v0.1 では `[]const u8` として扱います。
owned string は primitive ではなく、将来 `std::string` で扱います。

C ABI へ `std::string` を暗黙に渡してはいけません。
C へ渡す場合は、将来 `std::string::as_c_string` のような明示 API を使います。

v0.1 では collection runtime API を実装しません。
collection は次の順で検討します。

```text
array<T>         先に検討する owned contiguous collection
slice<T>         contiguous mutable view
slice<const T>   contiguous read-only view
map<K, V>        後続 phase
set<T>           後続 phase
```

## 15. concurrency / async 方針

Kizu v0.1 では `async fn` / `await` syntax は実装しません。

ただし、I/O と並行処理の境界は v0.1 から実装対象にします。
I/O は `Io` capability として明示し、並行処理は `Task` / `TaskGroup` で明示します。
`Io` / `Task` / `TaskGroup` は v0.1 では interpreter builtin から始めますが、
v0.1 のうちに `std::io` と `std::task` の API 境界へ寄せます。

```kizu
fn read_config(io: Io, path: []const u8) -> ![]const u8 {
    return fs.read_to_string(io, path)
}
```

方針:

* I/O する関数は `Io` を受け取る
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

v0.1 の最初の `TaskGroup` は interpreter 上の structured task model として実装します。
現在の `spawn` は OS thread や event loop を作らず、同期的に対象関数を評価して
`Task<T>` に結果を保持します。

v0.1 で追加していく concurrency foundation:

```text
std::task::Group          structured task scope
std::task::Queue          deterministic deferred task queue
std::task::parallel_for   safe data parallelism
std::task::parallel_map   disjoint partition output
std::channel::Channel<T>  owned message passing
```

`std::channel::Channel<T>` is owned message passing:

* `send(value)` moves non-copy values into the channel
* `recv()` returns an owned value
* borrow values and raw pointers cannot cross the channel boundary in safe Kizu
* v0.1 does not include `select`

`std::task::parallel_for` is safe data parallelism:

* v0.1 workers are `fn(i: i64) -> void` or `fn(i: i64) -> !void`
* `std::task::partition_mut(init: i64, count: i64)` creates disjoint `i64` output slots
* `partition.at(i)` reads or writes one checked slot
* `std::task::parallel_map(io, partition, start, end, worker)` writes `worker(i)` to slot `i`
* `std::task::LocalBuffer` is the trusted boundary for worker-local scratch
* first error propagation uses the existing `!void` / `try` model
* the interpreter may execute workers sequentially while preserving the API contract

Low-level concurrency boundary:

* `std::thread::scoped` is scoped and joined by construction
* `std::atomic::Atomic` is v0.1 seq_cst-only
* `std::sync::Mutex` is the explicit shared-mutable-state wrapper
* raw pointers cannot cross thread/task/channel boundaries in safe Kizu

OS thread、event loop、networking runtime、atomic ordering の詳細 API は、
safe structured API の後に追加します。
実並行 runtime を導入する場合も、上記の ownership / borrow / structured scope の制約を維持します。

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
        return os.write(self.fd, bytes)
    }
}
```

`Dyn<Contract>` は dynamic dispatch を型に見せます。

```kizu
fn save(writer: &Dyn<Writer>, bytes: &Bytes) -> !void {
    let n = writer.write(bytes)
    return void
}
```

v0.1 の `Dyn` は `&Dyn<Contract>` の動的 dispatch に限定します。
owned dynamic object、generic bounds、最適化された vtable layout は後続 phase で扱います。

## 17. ビルドとキャッシュ

Kizu の toolchain は、キャッシュが無制限に肥大化しない設計にします。

v0.1 の正として扱うコマンド:

```text
kizu parse
kizu check
kizu run
kizu fmt
```

`kizu fmt` は現時点では compact AST formatter output です。
完全な source-preserving formatter ではありません。

experimental tooling:

```text
kizu build
kizu ir
kizu cache status
kizu cache prune
kizu why-rebuild
kizu import-c-header
```

`kizu test` と `kizu lint` は v0.1 では未実装です。

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
fn main() {
    print("hello, kizu")
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
