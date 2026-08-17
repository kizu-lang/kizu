# Kizu 言語仕様

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

## 0. この仕様の範囲

この仕様は、今の Kizu を記述します。ここに書かれていない機能は、言語に無いものです。
将来入るかもしれない機能を範囲表で予告することはしません。

`kizu run` / `kizu check` / `kizu test` は生成した実行ファイルを走らせます。経路は
1 本で、interpreter はありません(ADR-0083)。実装は Go 一本です(ADR-0082)。

### 0.1 メモリ安全 gate

Kizu は safe Kizu のメモリ安全性を release blocker として扱います。
次は必ず守ります。

```text
use-after-move を許さない
double move を許さない
borrow 中の値の move を許さない
borrow escape を許さない
borrow を struct field に保存させない
borrow を comptime / unsafe 境界で延命させない
arena.get(handle) は local borrow だけを返す
別 arena の handle 使用を許さない
handle を raw pointer として扱わせない
unsafe の中でも type check / move check / borrow check を全面的に無効化しない
```

各項目は checker test または `examples/negative/` で検証します。
safe example は `kizu check` と `kizu run` の対象として維持します。

raw pointer operation、C ABI call、unchecked operation は safe Kizu の保証外です。
これらを使う場合、memory safety obligation はプログラマが負います。

allocator primitive と raw pointer runtime operation は完全実装ではありません。
実装済みの safe guarantee として扱ってはいけません。

### 0.2 まだ持たないもの

次は言語に無く、いま作る対象でもありません。永続的な非目標は「2. 非目標」にあります。

```text
full generics
type alias
complete fixed-width integer runtime semantics
float literals and float runtime arithmetic
overflow / truncation behavior for every numeric cast
raw pointer runtime operations
option<T> runtime helper
full stdlib
kizu lint
self-hosting compiler
async fn / await syntax
並行 API (task / channel / thread / mutex / atomic)
OS thread / event loop / networking runtime
Rust 同等以上の runtime performance guarantee
```

self-host は言語が固まってから、Go の構造に沿って作り直します(ADR-0082)。
thread は入れます。撤回したのは API の形だけで、実行系を先に作り、安全規則は
動く thread の上でだけ書きます(ADR-0025)。

## 1. 目標

Kizu は次を目指します。

- GCなしのメモリ安全性
- 単純な所有権
- move semantics
- borrowed view の戻り値の由来は署名から構造的に導出する(ADR-0098)
- borrow はローカル限定
- 書き方の自由度を増やしすぎない
- 標準ライブラリを厚めにする
- CIを速くする
- ビルドキャッシュを肥大化させない
- 依存グラフを抑える
- 隠れた制御フローを持たない

隠れた制御フローとは、呼び出しが source に見えないまま実行される制御の移動です。
呼び出しが source に明示されている限り、宣言の裏で body が生成されることは
hidden ではありません(ADR-0091)。

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

`[modules].root` は package 名そのものになる file を指します。省略できます。
library package はその module を持つ理由が無いことがあり、std は持ちません
(std の module はすべて `std::mem` 以下で、裸の `std` はありません)。

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

Kizu には borrow があります。ローカル borrow は `&T` / `&var T` で表します。
関数境界を越えて borrowed view を返す場合、由来は署名から構造的に
導出されます: 戻り値は view / borrow を運べる全引数に tied です(ADR-0098)。

borrow は次のことができません。

* struct / union の field に保存できない
* lexical block の外へ escape できない

長生きする関係は、参照ではなく次の型で表します。

```text
std::mem::Box<T>
std::arena::Arena<T>
std::arena::Handle<T>
```

## 5. 実装方針

実装は Go 一本です。言語とツールチェインの正は `internal/` と `cmd/kizu` にあり、
第二実装は持ちません(ADR-0082)。

`run` と `test` は生成した実行ファイルを走らせます。経路は 1 本で、interpreter は
ありません(ADR-0083)。native code は LLVM IR backend が生成します。

Go を選んだ理由は、実装が速く、依存を抑えやすく、CLI を作りやすいことです。

## 6. 基本文法

### 6.1 Hello world

```kizu
fn main() -> void {
    print("hello, kizu");
}
```

`main` は `void` または `<E>!void` を返します。値は返せません。
exit status は platform ごとに形が違い、返り値 1 つでは表せないためです(ADR-0085)。
error を返した `main` は診断を出して非ゼロで終了します。

### 6.2 変数

```kizu
fn main() {
    let name = "alice";
    var count = 0;
    count = count + 1;
    print(name);
    print(count);
}
```

`let` は immutable です。mutable な変数には `var` を使います。

使わない局所変数は compile error です。関数本体の binding は、その関数の中でしか
消費されないので、使われないものは確実に死んでいます。値を作ったこと自体が目的
なら `let _ = expr;` と書いて捨てます。

```kizu
fn main() {
    let values = make_values();   // error: 使われていない
    let _ = make_values();        // ok: 捨てると書いてある
}
```

関数の引数は対象外です。引数は署名の一部で、呼び出し側との約束のために受け取る
ことがあります。top-level 宣言も対象外です。package の外から使われる可能性が
あります。

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
simple statement の終端には `;` を必須にします。
対象は `let` / `var` declaration、assignment、`return`、`break` / `continue`、
expression statement です。
block statement、`if`、`while`、`for`、`match`、`comptime if` 自体には
終端 `;` を付けません。
comma-separated list は末尾カンマを許容します。

```kizu
fn bad_add(a: i64, b: i64) -> i64 {
    a + b; // error: non-void function must return explicitly
}
```

`void` 関数では `return` を省略できます。
早期 return が必要な場合は `return` を書きます。
`void` は値ではないため、`return void;` は使いません。
`!void` は error union なので、成功 path も `return;` で明示します。

```kizu
fn log(message: []u8) -> void {
    print(message);
}
```

同じ scope で、型が取った名前を関数は取れません。逆も同じです。

```kizu
struct Point { x: i64, y: i64 }

fn Point(x: i64) -> i64 {   // error: `Point` is a type in this scope
    return x + 100;
}
```

`Point(3)` を読んだ人は Point が構築されると読みます。名前が 2 つの無関係な
意味を同時に持つと、call site から意味を復元できません。戻り値をその型に
限る緩和も採りません。構築は `Point { x: 1, y: 2 }` で綴れるので、同じことを
する 2 つ目の綴りは要りません。

検査は宣言の順序に依存しません。struct と function のどちらが先に書かれていても
同じ error になります。std もこの規則の中にいて、例外を持ちません(§14)。

#### doc comment

declaration や member の user-facing documentation は `///` line comment で書きます。
`///` は通常の `//` comment とは区別され、直後の attachable item に
attached documentation として結びつきます。

attachable item は次です。

* function declaration
* method declaration
* `struct` / `enum` / `union` declaration
* struct field
* enum tag
* union variant

```kizu
/// Parses one source file into an AST.
/// Returns a parse error when the source is syntactically invalid.
pub fn parse_source(source: []u8) -> !Program {
    ...
}

/// Advances to the next token.
fn (self: &var Parser) advance() -> void {
    ...
}

/// Token produced by the lexer.
struct Token {
    /// Token kind.
    kind: TokenKind,
}

enum TokenKind {
    /// Identifier token.
    Ident,
}
```

attachment rule:

* doc comment は declaration の直前にある連続した `///` 行だけを対象にします
* 空行、通常の `//` comment、他の token が間にある場合は attach しません
* `pub`、`extern "c"` などの modifier は declaration の一部として
  扱い、その直前の `///` block を attach します
* 1 行ごとに先頭の `///` と、直後に 1 つだけある空白を取り除き、
  改行で連結します
* slash 3 つだけの `///` を doc comment とし、`//// text` は通常の line comment です
* `// SAFETY:` は「通常の `//` comment」ではないので、間に挟まっても attach を切りません
* block doc comment は採用しません

doc comment は ownership、name resolution、ABI、実行時挙動に影響しません。
型検査に対しても、書いてある内容は影響しません。ただし `unsafe fn` と
`unsafe struct` は doc comment の**存在**を要求します(§12)。private function にも
書けますが、doc comment の有無は visibility rule を変えません。
tooling は hover / completion / generated docs で attached documentation を表示できます。
最初の段落は summary として扱えますが、compiler diagnostic の意味づけには
使いません。

#### safety comment

`unsafe` を含む文の理由は `// SAFETY:` line comment で書きます。`///` が
「この宣言は何を約束するか」を書くのに対し、`// SAFETY:` は「なぜここで
コンパイラの証明を外してよいか」を書きます。

```kizu
// SAFETY: 直前に cap を確認済みなので len <= cap は保たれる
unsafe self.len = self.len + 1;
```

attachment rule:

* 接頭辞は ASCII 固定の `// SAFETY:` です。`//` と `SAFETY:` の間の空白は
  任意個で、後続の本文は自由記述です
* 文の直前にある連続した `// SAFETY:` 行だけを対象にします。空行、通常の
  `//` comment、他の token が間にあると attach しません。`///` は「通常の `//`
  comment」ではないので、間に挟まっても attach を切りません
* attach 先は式ではなく文です。1 つの文の中の `unsafe` はすべて同じ comment に
  結びつきます
* 外側の文に付けた comment は、その中の文には届きません

`unsafe` を含む文が `// SAFETY:` を持たないのは compile error です(§12)。
comment の内容は検査しません。

### 6.3.1 defer / errdefer cleanup statement

`defer <expr-stmt>;` は、現在の lexical block に明示 cleanup 呼び出しを登録します。
function body も block として扱います。

```kizu
import std::mem;
import std::array;

fn main() -> !void {
    let allocator = mem::page_allocator();
    let values = array::new<i64>(allocator);
    defer values.deinit();

    try values.append(1);
    return;
}
```

登録された cleanup は block を出るときに、登録順の逆順で実行します。
通常の block exit、明示 `return`、`try` などの error return path でも実行します。

`errdefer <expr-stmt>;` は、現在の lexical block から error return path で出る場合だけ
cleanup 呼び出しを実行します。通常の block exit や正常な `return` では実行しません。
`defer` と `errdefer` は同じ cleanup stack に登録し、block exit 時に登録順の逆順で評価します。
正常 exit では `errdefer` entry を skip します。

```kizu
fn make_values(allocator: mem::Allocator) -> !array::Array<i64> {
    let values = array::new<i64>(allocator);
    errdefer values.deinit();

    try values.append(1);
    return values;
}
```

で許可する形は cleanup method call の expression statement だけです。

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
`errdefer` receiver は、その `errdefer` が実行され得る各 error exit path で同じ rule を満たす
必要があります。成功 path で owner を move / return することは `errdefer` の実行を要求しません。

### 6.4 struct

```kizu
struct User {
    name: []u8,
    age: i64,
}
```

struct literal は、その struct が宣言する field をちょうど一度ずつ書きます
([ADR-0079](docs/adr/0079-struct-literal-field-initializers.md))。

```kizu
let user = User { name: "alice", age: 30 };
```

* 宣言されていない field 名は compile error です
* 宣言された field を書かないのは compile error です
* 同じ field を二度書くのは compile error です。後勝ちにはしません
* 書く順序は自由です。field は名前で宣言に対応づけます

```kizu
let user = User { name: "alice", age: 30, age: 31 }; // error: duplicate field `User.age`
```

コンパイラが検査できない不変条件を field に持つ struct は `unsafe struct` で
宣言します。規則は §12 にあります。

### 6.5 namespace access

Kizu は型や名前空間に属する item lookup に `::` を使います。

```kizu
let color = Color::Red;
let shape = Shape::Circle(10);
let handle = io::blocking();
let text = try fs::read_file(handle, allocator, "config.toml", mem::Limit::Unlimited);
```

`.` は runtime value の field / method access だけに使います。

```kizu
print(user.name);
let handle = users.add(user);
self.related.len();
```

`Color.Red` や `Shape.Circle(10)` のような dot による enum / union lookup は
compile error です。互換構文としては扱いません。

method receiver path は local binding または one-level direct field に限定します。

```kizu
values.len();          // ok: local receiver
self.related.len();    // ok: direct field receiver
self.a.b.len();        // error
```

direct field receiver は owner の ownership state に従います。read-only method は
owner / field が読めるときだけ、mutating method は owner / field が borrow 中でないときだけ
呼べます。`field.deinit()` のような destructive cleanup は、owner 型自身の
`deinit(self: Owner) -> void` method body の中だけ許可します。

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
wildcard import、relative import、re-export、alias import は扱いません。
cyclic import は compile error です。

import した path は最後の segment 名で束縛され、その名前で参照します。

```kizu
import app::compiler::lexer;

let tokens = try lexer::lex(source);
```

std も同じ規則です。import せずに `std::` で始まる名前は書けません。

```kizu
import std::fs;

let text = try fs::read_file(io, allocator, "config.toml", mem::Limit::Unlimited);
```

package 根も import できます。根を import すると根の名前が束縛され、その下の
module へは完全パスで辿ります。どちらの粒度で import するかは file ごとに
選びます。module 名と同じ名前の値を置きたい file は、根で import します。

```kizu
import std;

let io = std::io::blocking();
```

module の完全パス(`std::fs`)は、その module の同一性を指す名前です。import 宣言と
文書に現れます。コード中の参照は、束縛された名前から始まります。

user package に `std` という名前は使えません。
std package 内部の module も同じ規則で import します。`std::internal::builtin` は
`internal` の下にあるので、std package の中からだけ import できます。

name resolution order:

1. local bindings
2. current module top-level declarations
3. imported names by last segment
4. builtin(14 章の `print`)
5. error

同じ last segment を持つ import は compile error です。
local declaration が import した名前を shadow することも compile error です。
使わない import も compile error です。import 一覧がその file の依存一覧である
ことは、これが保証します。

package の内部 module は `internal` ディレクトリで隠します。manifest に一覧は
書きません。どこに置いたかが規則そのものです。

```text
src/lexer.kizu                   -> app::lexer                    公開
src/internal/table.kizu          -> app::internal::table          app 配下からだけ
src/parser/internal/state.kizu   -> app::parser::internal::state  app::parser 配下からだけ
```

`internal` は階層のどこにでも置けます。`X::internal::Y` は `X` とその下の module
からだけ import / 参照できます。部分木の中だけで使う内部 module を、package 全体に
見せずに置けます。

visibility は default private です。

```kizu
import std::array;

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

pub fn lex(source: []u8) -> !array::Array<Token> {
    return lex_source(source);
}

fn lex_source(source: []u8) -> !array::Array<Token> {
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
* package 外から見える API は `internal` の下にない module の `pub` declaration に限る
* `pub(crate)`、`pub(super)`、`protected` は採用しない

### 6.7 enum

Kizu の `enum` は Zig/C 寄りの tag enum です。
Rust の payload enum / algebraic data type とは分けます。

enum は、payload を持たない named tag だけを実装します。

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
    Label([]u8),
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

`union` は次に限定します。

* variant ごとの payload は0個または1個
* pattern guard はない
* destructuring は payload binding 1つだけ
* `match` は exhaustive でなければならない

payload binding の所有は、payload の型と match される値の所有で決まります
(ADR-0090)。

* scalar(bool / 整数 / float / enum / error set)の payload は copy として
  束縛され、どの match からでもそのまま使えます。
* 宣言された struct / union 型の payload は、owned なローカル値または
  呼び出し結果の temporary への match で move out として束縛されます。
  move する arm が 1 つでもあれば、match 以降そのローカル値全体は moved に
  なります。borrow への match では move out できず、payload は borrow として
  束縛されます。
* `[]T` や raw pointer などの view 型 payload は常に borrow として束縛され、
  arm の外へ escape できません。

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
branch value は expression なので `;` を付けません。
`let` / `var` initializer、assignment、または expression statement として `if` expression を使う場合は、
外側の statement を `;` で終端します。

```kizu
let label = if age >= 20 {
    "adult"
} else {
    "minor"
};
```

三項演算子は採用しません。

optional 条件(§7)には payload capture を書けます。capture は statement
形だけで、consequence の中でだけ見えます。expression では `orelse` を
使います。

```kizu
if find(text, b) |at| {
    print(at);
} else {
    print(-1);
}
```

### 6.9.1 bool 演算

Kizu は boolean logic に `and` と `or` を使います。
両辺は `bool` でなければなりません。
`and` と `or` は短絡評価します。

優先順位は低い順に次の通りです。

```text
orelse
or
and
== !=
< <= > >=
```

`opt orelse default` は optional の値、無ければ default を返します。
左辺は `?T`、右辺と結果は `T` です。右辺は左辺が null のときだけ評価
されます。

右辺には default の代わりに `return [expr]` / `break [:label]` /
`continue [:label]` を書けます。null なら enclosing 関数・loop を離れる
ので、式自体は常に payload を生みます。miss の早期 return を capture の
入れ子なしで書く guard 形です。

```kizu
let at = find(text, b) orelse return -1;
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

Kizu は `loop` keyword を採用しません。
無限 loop は `while true` と書きます。

```kizu
while true {
    break;
}
```

optional 条件には payload capture を書けます。値がある間 loop し、
payload は毎周 capture に束縛されます。`?T` を返す `next()` がそのまま
iterator protocol になる形です。

```kizu
while it.next() |byte| {
    print(byte);
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

`for` は、i64 の half-open range に限定します。
終了値は含みません。

```kizu
for 0..3 |i| {
    print(i);
}
```

iterator protocol、collection iteration、`inline for` は扱いません。

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

guard と多段 destructuring は扱いません。
tagged union の payload binding だけを扱います。
duplicate arm、unknown tag、non-exhaustive match は compile error です。
すべての `match` arm は terminal comma を必須にします。
wildcard pattern `_` を fallback arm として許可します。
`_` arm は最後に 1 つだけ書けます。payload binding はできません。
`_` arm がある場合、明示されていない残りの tag を束ねるため exhaustive とみなします。
`_` arm がない場合は、すべての tag を明示しなければなりません。
expression として使う場合は、すべての arm の value type が一致しなければなりません。
arm value は expression なので `;` を付けません。

statement として使う `match` の arm body は、expression または `return` 文です
(ADR-0093)。`Tag => return,` は関数からの早期 return で、payload なし variant の
「何もしない」を明示する書き方でもあります。expression として使う `match` の
arm に `return` は書けません。

```kizu
fn (self: Slot) deinit() -> void {
    match self {
        Kept(payload) => payload.deinit(),
        Vacant => return,
    }
    return;
}
```
`let` / `var` initializer、assignment、または expression statement として `match` expression を使う場合は、
外側の statement を `;` で終端します。

```kizu
let label = match color {
    Red => "red",
    Green => "green",
    Blue => "blue",
};
```

## 7. 型

基本型:

```text
bool
void
```

`i64` は整数 literal のデフォルト型です。
Kizu は `int` のような幅が曖昧な整数型を導入しません。

文字列 literal の型は `[]u8` です。
`string` primitive は導入しません。

文字列 literal には次の 2 形式があります。

* **single-line literal** `"..."`：1 行に収まる literal。escape 解釈は行わず、二重引用符の間のバイト列がそのまま値になります。
* **multi-line literal** `\\<content>`：行頭の `\\` (backslash 二つ) で始まる行を 1 つ以上連続させると、`\n` で連結された 1 つの literal になります。delimiter は行末で閉じるため末尾編集で誤って閉じ忘れる事故が起きません。連続性は「次の非空白文字が `\\` で始まる行か」で判定し、空白行や注釈行は許容しません。

```kizu
let help =
    \\Usage: kizu <command>
    \\
    \\Commands:
    \\  build    Build the project
;
```

multi-line literal の値は連結後のバイト列 (`Usage: kizu <command>\n\nCommands:\n build Build the project`) で、`[]u8` 型として `"..."` と相互に交換可能です。

`void` は値を返さない関数の戻り値です。
Kizu は `Unit` という別名を導入しません。

低レベル型として、次の明示幅整数、浮動小数点、raw pointer 型名を予約します。
主に checker / `unsafe` / extern declaration のために扱います。
完全な fixed-width arithmetic、float literal、overflow semantics はまだ扱いません。

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

type alias は持ちません。導入するかどうかは未検討です。

**optional 型 `?T`** は「値が無いかもしれない」を型にします(ADR-0101)。
不在は error ではありません: 回復する失敗は `E!T`、正常な不在(検索の
miss、iterator の終端)は `?T` と使い分けます。

```kizu
fn find(bytes: []u8, needle: u8) -> ?i64 {
    ...
    return index;    // T は ?T へ暗黙に wrap(!T の成功と同じ規則)
    ...
    return null;     // 不在
}

if find(text, b) |at| { print(at); }     // payload capture(§6.9)
let at = find(text, b) orelse -1;        // 既定値(§6.9.1)
while it.next() |byte| { ... }           // 終端まで loop(§6.10)
```

`null` は文脈が `?T` の位置(return、引数、`orelse` の右辺以外の対象
位置)でだけ書けます。payload に触る手段は capture と `orelse` の 2 つ
だけで、presence を確かめない取り出しはありません。

error union との合成 `E!?T` / `!?T` は書けます: 呼び出しは失敗しうるし、
成功しても値が無いかもしれない。`try` は error 層だけを剥がして `?T` を
返すので、`while try s.next() |byte|` や `try s.next() orelse 0` と
そのまま組み合わせられます。

```kizu
fn (self: &var Stream) next() -> ReadError!?u8 {
    if self.broken { return ReadError::Broken; }  // 失敗
    if self.done() { return null; }               // 正常な終端
    return byte;                                  // 値(二重 wrap は暗黙)
}
```

element の規則は error union の成功型と同じです: parse できる型なら
書けます(view `?[]u8`、owner `?String`、generic `?T`、struct、enum)。
payload の階級は型が運びます: owner payload は capture / `orelse` の
結果に消費義務が付き、view payload は view の借用規則にそのまま従い
ます。owner / view を包んだ optional は「生まれた場所で消費」限定で、
let / var への保存と引数渡しは copy element の optional だけができます。

owned container(`Map` / `Array` / `String` / `Box` / stack buffer)を読んだ
呼び出しが返す view payload は、さらに capture 限定です: capture は条件式が
読んだ container を共有借用し、view の最終使用まで mutation と deinit を
待たせます。`orelse` は container に紐づかない裸の view を作るため拒否されます
(`let view = string.as_bytes()` が let 限定なのと同じ理由の位置制限です)。

borrow payload(`?&T` / `?&var T`)は最も強い階級で、`Array.at` /
`Array.at_mut`、`Map.at` / `Map.at_mut` の capture 条件としてだけ存在
します。capture が payload borrow そのものになり、その scope の間
container は borrow されます。保存・`orelse`・signature への出現はすべて
拒否します(§std array、§std map)。

現在の制限:

* `??T` / `?!T` は書けない(optional を包めるのは error union だけ)。
  generic 実体化が作る綴り(`Array<!i64>` の `pop()` が返す `?!i64`)も
  同じ規則で拒否される
* struct field・union payload・static argument(`Array<?u8>` など)・
  borrow(`&?T`)の対象にはできない
* `?ptr<T>` は raw pointer の nullable 綴りのままで、この optional
  semantics の対象外(unsafe 世界の C ABI 用)

collection は primitive ではなく、標準ライブラリ型として扱います。
実装済みの collection / ownership 型:

```text
std::array::Array<T>
std::map::Map<K, V>
std::string::String
std::mem::Box<T>
std::arena::Arena<T>
std::arena::Handle<T>
```

まだ持たない ownership / container 型:

```text
std::set::Set<T>
shared<T>
[]T (u8 以外の一般 slice)
```

full generics を実装しません。
`std::arena::Arena<T>`、`std::arena::Handle<T>` は compiler-known な stdlib 型コンストラクタです。
`!T` と raw pointer 型は専用の型構文として扱います。
ADR-0066 の最小明示 function generics だけを採用します。

### 7.1 index / slice expression

Kizu は checked index と checked slice syntax を持ちます。

```kizu
let byte = bytes[index];
let part = bytes[start..end];
let tail = bytes[start..];
let head = bytes[..end];
```

最初の対象は `[]u8` です。

```text
[]u8 [ i64 ] -> u8
[]u8 [ i64 .. i64 ] -> []u8
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

writable slice place(`&var []u8` binding)への indexed assignment
`buf[i] = x` は許可します。bounds は読みと同じく trap です。
書き込みの供給源は §9 の mutable view 規則が定めます。
indexed borrow、multi-dimensional slicing、
`std::array::Array<T>` への直接 indexing は後続に分離します(ADR-0096
決定 3: Array は std 定義の struct であり、組み込み indexing は IR の
layout 結合か隠れ call になるため)。

固定長の stack buffer は `[N]u8` です(ADR-0097)。N は正の整数 literal で、
`var buf = [64]u8{};` が zero 埋めで生成します。view の入口は
`buf.as_bytes()` / `buf.as_mut_bytes()` で、規則は `String` の同名 method と
同じです(束縛必須、`as_mut_bytes` は mutable binding 限定で exclusive)。
stack buffer は local 限定です: struct field / union payload / parameter /
返り値 / container element に置けず、`&` / `&var` で直接 borrow できません。
関数境界へは view を渡します。element は `u8` だけで、buffer への直接
indexing は持ちません(書き込みは view 経由の 1 経路)。stack buffer は
owner ではなく、`deinit` は不要です。

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
[]u8 -> numeric
bool -> numeric
numeric -> pointer
pointer -> numeric
pointer -> pointer
```

raw pointer 間の cast は `unsafe` マーカーを要求します。
integer と raw pointer の変換は `cast<T>` では扱わず、専用 primitive として
同じくマーカーを要求します。

```kizu
fn write_as_mut(p: ptr<const u8>) {
    let q = unsafe cast<ptr<u8>>(p);
    unsafe ptr_write(q, 1);
}
```

pointer cast の memory safety obligation はプログラマが負います。
ただし、`unsafe` の中でも type check / move check / borrow check は無効化されません。

## 8. 所有権

所有される値を代入または関数引数として渡すと move されます。

copy 型:

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
std::array::Array<T>
std::map::Map<K, V>
std::string::String
std::mem::Box<T>
arena-owned value
non-copy field を含む struct
```

`std::mem::Box<T>` は型として存在しますが、native lowering はまだありません。

owner field または owner payload を含む struct / union は owner aggregate です。
owner aggregate は copy できず、値渡しや代入では move されます。
block を出る時点で、owner 値は次のいずれかで consume 済みでなければ
compile error です(ADR-0091)。

* `value.deinit()` または `defer value.deinit();` による明示 cleanup
* 別の owner aggregate / container への move
* owned return value として caller への move
* `std::mem::leak(value)` による明示 leak

owner aggregate を値引数として受け取る関数は、その値を consume する義務を負います。
読み取りだけを行う関数は `&T` で受け取ります。
mutation が必要な関数は `&var T` で受け取り、consume する関数は owner aggregate を値で受け取ります。

owner field または owner payload を含む型は `deinit(self: T) -> void` を必須とし、
cleanup contract を source 上に見えるようにします。
`deinit` body 内では `self.field.deinit()` のような direct field cleanup を許可し、
body は self の owner field をすべての path で consume しなければなりません。
`deinit` の外では owner field を個別 cleanup して部分破壊状態を露出させてはいけません。
要素が owner 型の container は shallow な `deinit()` を型 error とし、要素ごと
consume する `deinit_all()` だけを cleanup として認めます。空の container への
`deinit_all()` は合法です。`deinit_all` は要素を要素自身の `deinit()` で consume
するため、owner 要素の container を直接要素にする入れ子は構築時に型 error です。
deinit を宣言した struct で包んで名前を与えます。owner 要素の `set` も、置き換え
前の要素を leak するため型 error です。Arena は要素の deinit を実行しないため、
owner 型を要素にできません。

owner payload を持つ `union` も owner aggregate です。その `deinit` は active variant の
payload だけを、通常は exhaustive な `match` で cleanup します。inactive variant の
payload storage は cleanup しません。tag が初期化済みと示す payload だけを処理します。
inline payload の size と alignment は compile time に確定している必要があります。

```kizu
union MirStmt {
    LetCall(MirLetCall),
    ReturnExpr(MirReturnExpr),
    If(MirIf),
}

fn (self: MirStmt) deinit() -> void {
    match self {
        LetCall(stmt) => stmt.deinit(),
        ReturnExpr(stmt) => stmt.deinit(),
        If(stmt) => stmt.deinit(),
    }
}
```

## 9. borrow

borrow は一時的に値を参照するための仕組みです。

```kizu
fn show(s: &[]u8) {
    print(s);
}
```

mutable borrow には `&var T` を使います。

```kizu
fn update(user: &var User) -> void {
    user.name = "bob";
}
```

borrow のルール:

* borrow は一時的
* local borrow binding は straight-line code では最後に使った場所で終了する
* borrow argument は呼び出し statement の終了で終了する
* borrow field は struct / union に保存できない
* borrow 中の値は move できない
* `&T` と `&var T` は重複できない
* `&var T` 同士は同じ値に対して重複できない
* `&var T` argument は mutable local binding に限定する
* `&user.name` のような one-level direct field borrow を許可する
* field borrow 中でも disjoint field assignment は許可する
* field borrow 中の owner 全体の move と同一 field assignment は禁止する
* `&user.profile.name` のような nested field borrow を拒否する
* index / slice expression は read-only checked access から始める
* indexed borrow syntax はまだ実装しない。将来 `&items[0]` を追加する場合は、
  専用の安全ルールと regression coverage を先に追加する

境界を越える borrowed view の由来は、注釈でなく署名から構造的に
導出されます(ADR-0098):

```kizu
fn first(bytes: []u8) -> []u8            // 戻り値は bytes に tied
fn show(value: &i64) -> &i64             // 戻り値は value に tied
fn pick(a: []u8, b: []u8, f: bool) -> []u8   // 戻り値は a と b の両方に tied
```

規則は 1 つです: **戻り値は、その型が構造的に運べる範囲で、view / borrow を
運べる全引数(と `self` receiver)に tied**。

* scalar / bool / enum の戻り値は tie を運べず、常に自由です
* view / borrow の戻り値は、view / borrow 引数の保守的統合に tied です。
  checker はどの引数が選ばれたかを追跡せず、戻り値が生きている間は
  全 source を borrow 中として扱います
* `&var T` の戻り値の source は `&var` 引数に限ります(`&T` からは作れない)
* mutable な戻り値では全 source が exclusive borrow になるため、
  同じ値を 2 つの source 位置に渡せません
* `Allocator` も tie を運びます。tied な `Allocator` 引数(§15.3)を受けて
  scalar 以外を返す関数の戻り値は、その allocator の tie を継承します。
  tie のない allocator からは何も継承せず、既存コードの意味は変わりません
* **view を持てる struct** も tie を運びます(ADR-0100)。全 field が
  copy 値・view・そのような struct で、transitively `[]u8` field を含む
  型がこれに当たります。この型を返す関数の戻り値は、borrow-class な
  view 引数(local view binding、または捕捉済み struct の view field)の
  保守的統合に tied です。source が無ければ通常の値で、既存コードの
  意味は変わりません

view を持てる struct は、`let` / `var` binding の初期化位置でだけ
local view を捕捉できます:

```kizu
struct BytesIter { pub bytes: []u8, pub index: i64 }
let it = BytesIter { bytes: view, index: 0 };   // it は view の source に tied
var it2 = iter(view);                            // 関数経由でも同じ
```

捕捉した binding は borrow class に入ります: frame から escape できず
(return、move、struct への再格納は拒否)、source が生きている間
source は borrow 中で、binding は最後の使用で終了します。owner field を
持つ struct は捕捉できません(borrow class は deinit 義務を運ばない
ため)。borrow-class 値の `[]u8` field 読みは let では同じ tie を継ぎ、
move 文脈では escape として拒否します。source が関数 parameter だけの
捕捉は自由な値のままです: parameter は frame より長生きし、呼び出し側が
署名から tie を再導出します。

契約は署名だけから導出され、body は参照されません。
名前付き lifetime parameter、lifetime bounds、anonymous lifetime は
採用しません。borrow field(`&T` の field 保存)は採用せず、view の
struct 捕捉は上記の view-capture 規則が担います。

safe borrow binding は通常の field access 構文で field を読めます。
`&var T` binding は通常の field assignment 構文で field を更新できます。

可変性は型でなく borrow が運びます(ADR-0096)。view 型は `[]u8` の 1 つ
だけで、`&var []u8` として view を持つときだけ要素書き込み `buf[i] = x` が
できます。許すのは要素書き込みだけで、`buf.* = other` による view の
差し替えはできません。`&var []u8` を作れるのは書き込み可能な place だけ
です: `String.as_mut_bytes()` と stack buffer の `as_mut_bytes()`
(どちらも mutable binding から。view が生きている間、元の値全体が
exclusive borrow)、および `&var []u8` 引数の
再貸し。plain `[]u8` からは作れず、`var` 束縛の plain slice local も
`&var []u8` parameter には渡せません(backing の書き込み可能性を保証
しないため)。可変 view は borrow なので field に保存できず、escape
できません。

view binding(shared / writable どちらも)は、**callee が view を statement を
越えて保持できない**関数の plain `[]u8` 引数に貸せます: 戻り値の型が view を
運べず(scalar / void / view を持てない struct)、かつ view を保持できる型の
`&var` parameter が無いこと(`&var []u8` は差し替え不可のため除外)。貸与は
呼び出し statement の終了で終わります。view を捕捉し得る struct を返す
関数へは、tie を記録できる `let` / `var` 初期化の位置でだけ渡せます。
それ以外の位置では borrow の追跡が失われるため escape として拒否します。
safe borrow は実装上 pointer-like な表現を持ち得ますが、言語上は
checker が lifetime、aliasing、move を検査する borrow capability です。
raw pointer はこの省略対象ではなく、`unsafe` マーカーで明示的に扱います。

```kizu
fn show(user: &User) -> void {
    print(user.name);
}

fn rename(user: &var User) -> void {
    user.name = "bob";
}
```

明示 dereference は postfix の `.*` を使います。safe borrow では borrow
そのものを読む場合や、field access ではない dereference を表す場合に使います。

```kizu
fn value(read: &i64) -> i64 {
    return read.*;
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
    name: []u8,
    age: i64,
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
* `&var T` 経由の dereference assignment は許可
* `&T` 経由の field assignment は禁止
* `&var T` 経由の field assignment は許可

```kizu
fn main() -> void {
    var user = User { name: "alice", age: 30 };
    user.name = "bob";
}
```

## 10. arena / handle

Kizu は、長寿命の参照を複雑な lifetime で表さず、`std::arena::Arena<T>` と `std::arena::Handle<T>` で表します。

```kizu
import std::mem;
import std::arena;

let allocator = mem::page_allocator();
let users = arena::new<User>(allocator);
let alice = users.add(User { name: "alice" });
print(users.get(alice).name);
```

`std::arena::Arena<T>` は複数の `T` を所有します。
core arena の構築は明示 allocator capability を要求し、
`std::arena::new<T>()` は無効です。allocator 引数は読み取りとして扱われ、move されません。

`std::arena::Handle<T>` はポインタではありません。arena 内の値を指す opaque な ID です。

ルール:

* `std::arena::new<T>(allocator)` は `Allocator` を明示して `std::arena::Arena<T>` を作る
* `std::arena::Arena<T>.add(value)` は value を arena に move する
* `std::arena::Arena<T>.add(value)` は `std::arena::Handle<T>` を返す
* `std::arena::Arena<T>.get(handle)` はローカル borrow を返す
* `std::arena::Arena<T>.deinit()` は arena を明示 cleanup し、binding を無効化する
* `std::arena::Arena<T>.deinit()` は owned local receiver の 0 引数呼び出しだけを許可する
* `owner.field.deinit()` は owner 型自身の `deinit(self: Owner) -> void` method 内だけ許可する
* handle は borrow より長生きしてよい
* handle は対応する arena より長生きしてはいけない
* `deinit` 後の arena と、その arena 由来の既知 handle は使用してはいけない
* handle は raw pointer ではない
* arena からの削除は実装しない

## 11. エラー処理

Kizu は exception を使いません。
error は値として扱います。

Zig に近い `!T` を導入します。
`!T` は「成功時は `T`、失敗時は error」を表します。
error 値は名前であり、payload を持ちません(ADR-0086)。
失敗の種類は `error` set として宣言し、member を返します。

成功時は通常の `T` をそのまま `return` します。

```kizu
error ParseError {
    Bad,
}

fn parse() -> !i64 {
    return 1;
}

fn fail() -> !i64 {
    return ParseError::Bad;
}
```

`try` は `!T` を unwrap します。
error の場合は、現在の関数からその error value を返します。

```kizu
fn next() -> !i64 {
    let value = try parse();
    return value + 1;
}
```

失敗の詳細(path、期待値、実際の値)を運びたい場合、error union には載せません。
payload を持つ結果は通常の `union` を戻り値として返し、`match` で分岐します。

typed error を `try` で伝播する例:

```kizu
error ConfigError {
    NotFound,
    InvalidPort,
}

fn read_port(ok: bool) -> ConfigError!i64 {
    if ok {
        return 8080;
    }

    return ConfigError::NotFound;
}

fn main() -> ConfigError!void {
    let port = try read_port(true);
    print(port);
    return;
}
```

### 6.14.1 error set

失敗の種類は `error` で宣言します。

```kizu
error FsError {
    NotFound,
    Denied,
}

fn read(path: []u8) -> FsError!i64 {
    return FsError::NotFound;
}
```

error 値は set の member そのものであり、**payload を持ちません**。error は
「何が起きたか」だけを運び、位置や期待値のような詳細は diagnostic が持ちます
(ADR-0086)。

* `error Name { A, B }` で宣言する
* member は `Name::A` で参照する
* `!T` は error set を宣言しない error union で、あらゆる set の member を受け取る
* `E!T` は宣言した set の member だけを受け取る
* 宣言した set は `match` で網羅的に分岐できる。`!T` は set を持たないので分岐できない
* error 値が `main` から出た場合、`runtime error: Name::A` として報告される

ルール:

* `try` は `!T` を返す関数内でだけ使える
* `try` の operand は `!T` でなければならない
* `E!T` の `E` は宣言済みの `error` set でなければならない
* `E!T` では `E` の member または `T` を返せる
* `!T` は set を宣言しないので、body はどの set の member でも伝播・返却できる
* `E!T` と宣言した場合、`try` は同じ `E` だけを伝播できる
* `!T` 関数では `T` を返すと成功値、error set の member を返すと失敗値として扱う
* error 値は大域一意な整数 1 個に lower される。set をまたぐ変換は存在しない
* `!void` の成功 return は `return;` と書く
* exception / stack unwinding は使わない
* `option<T>` は型名として予約するが、runtime helper を実装しない

## 12. unsafe / C ABI

`unsafe` は、コンパイラが memory safety を証明しない操作を式単位で明示する
マーカーです。`try` / `comptime` と同じく式の前に置きます。

```kizu
unsafe ptr_write(p, 20);
```

マーカーは値を持ちません。`unsafe E` は「E の中でコンパイラが証明しない操作の
memory safety obligation を、書いた人が負う」という宣言であり、E の型も値も
変えません。

マーカーは覆った式の中の**すべて**の未証明操作を覆います。

```kizu
unsafe ptr_write(dst, ptr_read(src));   // ptr_write と ptr_read の両方を覆う
```

呼び出し側に obligation を要求する関数は `unsafe fn` で宣言します。`unsafe fn`
の本体は通常の関数本体であり、本体の未証明操作にはそれぞれ `unsafe` が要ります。

```kizu
unsafe fn raw_write(p: ptr<u8>, value: u8) -> void {
    unsafe ptr_write(p, value);
}

fn caller(p: ptr<u8>) -> void {
    unsafe raw_write(p, 1);
}
```

コンパイラが検査できない不変条件を持つ struct は `unsafe struct` で宣言します。
raw pointer を field に持つ struct は `unsafe struct` でなければ compile error です。

```kizu
/// data は少なくとも len バイトの読み出し可能なメモリを指す。
unsafe struct Bytes {
    data: ptr<const u8>,
    len: usize,
}
```

`unsafe struct` は field を `pub` にできません。Kizu の module は 1 file で
`pub(crate)` / `pub(super)` を持たないため(§6.6)、不変条件を壊しうるコードは
宣言と同じ file の中だけに閉じます。

不変条件を作る操作 —— 構築と field への書き込み —— には `unsafe` が要ります。
field の読みには要りません。読み出した raw pointer は、それを使う操作の側が
`unsafe` を要求します。

```kizu
fn size(b: &Bytes) -> usize {
    return b.len;
}

fn shrink(b: &var Bytes, n: usize) -> void {
    unsafe b.len = n;
}

fn wrap(p: ptr<const u8>, n: usize) -> Bytes {
    return unsafe Bytes { data: p, len: n };
}
```

義務を作る宣言 —— `unsafe fn` と `unsafe struct` —— には `///` が要ります。義務の
中身はコードに書けず、書ける場所は comment だけです。何が書いてあるかは検査しませんが、
何も書いていないことは検査します。

義務を果たす場所 —— `unsafe` を含む文 —— には直前の `// SAFETY:` が要ります。

```kizu
/// p は少なくとも 1 バイトの書き込み可能なメモリを指していなければならない。
unsafe fn raw_write(p: ptr<u8>, value: u8) -> void {
    // SAFETY: p の有効性は呼び出し側が保証する(unsafe fn の契約)
    unsafe ptr_write(p, value);
}
```

接頭辞は ASCII 固定の `// SAFETY:` です。後続の本文は自由記述で、日本語で構いません。
単位は式ではなく文なので、1 つの文に `unsafe` が複数あっても comment は 1 つで足ります。

```kizu
// SAFETY: dst と src はどちらも 1 バイト分有効であると呼び出し側が保証する
unsafe ptr_write(dst, unsafe ptr_read(src));
```

C ABI declaration は `extern "c" fn` で書きます。

```kizu
extern "c" fn puts(s: ptr<const u8>) -> i32
```

ルール:

* 未証明操作は `unsafe` マーカーに覆われていなければ compile error
* マーカーが未証明操作を 1 つも覆っていない場合は compile error。
  内側の `unsafe` が唯一の操作を覆っている場合、外側のマーカーは未使用になる
* マーカーの単位は式である。`unsafe p.* = value` はマーカーが代入先を覆う。
  代入する値は別の式なので、それ自体が未証明ならそちらにもマーカーが要る
* `extern "c" fn` の呼び出しには `unsafe` が要る
* `unsafe fn` の呼び出しには `unsafe` が要る
* `unsafe fn` の本体は暗黙に覆われない
* `ptr<T>` は non-null mutable raw pointer
* `ptr<const T>` は non-null const raw pointer
* `?ptr<T>` / `?ptr<const T>` は nullable raw pointer
* safe borrow と raw pointer は別物として扱う
* `p.*` は `ptr<T>` / `ptr<const T>` から `T` を読む
* `p.* = value` は `ptr<T>` に `T` を書く
* `p.*.field` は struct raw pointer の field read / assignment に使える
* `ptr<const T>` 経由の assignment は禁止
* `?ptr<T>` / `?ptr<const T>` は直接 dereference できない
* `p.field` のような raw pointer field access は禁止
* `ptr_read(p)` は `ptr<T>` / `ptr<const T>` から `T` を読む
* `ptr_write(p, value)` は `ptr<T>` に `T` を書く
* `ptr_write` は `ptr<const T>` と nullable pointer には使えない
* raw pointer を field から読み出すのに `unsafe` は要らない。取り出した
  pointer を使う操作の側が要求する
* `ptr<T>` / `?ptr<T>` を field に持つ struct を `unsafe struct` と宣言しないのは
  compile error
* `unsafe struct` の field に `pub` は付けられない
* `unsafe struct` の構築と field への書き込みには `unsafe` が要る
* `unsafe fn` / `unsafe struct` に `///` が無い、または本文が空なのは compile error
* `unsafe` を含む文の直前に `// SAFETY:` が無い、または本文が空なのは compile error
* `// SAFETY:` は文に付く。外側の文の comment は内側の文には届かない
* `// SAFETY:` と文の間に空行や他の comment を挟むと、その comment は届かない

```kizu
struct Node {
    tag: i64,
}

fn update(node: ptr<Node>) -> void {
    unsafe node.*.tag = 1;
}
```

`unsafe` を要求する操作の種類:

| 種類 | operation |
| --- | --- |
| `ptr_read` | `ptr_read(p)` |
| `ptr_write` | `ptr_write(p, value)` |
| `ptr_deref` | `p.*` / `p.* = value` / `p.*.field` |
| `ptr_cast` | raw pointer 間の `cast<ptr<...>>(value)` |
| `ptr_int_cast` | `ptr_from_int<ptr<...>>(value)` / `int_from_ptr<usize>(value)` |
| `extern_call` | `extern "c" fn` call |
| `unsafe_call` | `unsafe fn` call |
| `struct_invariant` | `unsafe struct` の構築 / field write |
| `volatile` | volatile read/write primitive |

この表の名前はソースには書きません。マーカーは `unsafe` の 1 語で、種類は
式の綴りが名乗ります(`ptr_read(p)`、`p.*`、`cast<ptr<u8>>(p)`)。綴りから
読めない `extern_call` / `unsafe_call` / `struct_invariant` は、呼び先や書き込み先の
宣言(`extern "c" fn` / `unsafe fn` / `unsafe struct`)と module path が名乗ります。名前が残るのは診断メッセージの中だけで、
マーカーが無いときにどの種類の操作だったかを伝えます。

`atomic`、`unchecked_index` は採用しません。
`volatile` は compiler / CPU に対する通常の最適化抑制・順序制約を表す primitive であり、
thread synchronization ではありません。atomic operation とは別に扱います。

unsafe code の memory safety obligation はプログラマが負います。
ただし、`unsafe` は compiler check を全面的に無効化するものではありません。

`unsafe` の中でも次は error のままです。

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
これは experimental です。

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
    x: i32,
    y: i32,
}
```

または:

```kizu
@repr("c")
struct Point {
    x: i32,
    y: i32,
}
```

C layout struct は `unsafe struct` の要求から外します。C ABI struct は field を
`pub` にできないと構築できず、名前も C の側が決めるためです。raw pointer field を
持つ C layout struct が safe Kizu の保証外である根拠は §0.1 が既に持っています。

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

最小構文:

```kizu
let size = comptime 4 * 1024;
```

compile-time の値は static 引数リスト `<...>` に書きます。

```kizu
fn sized<n: i64>() -> i64 {
    return n;
}
```

`(...)` には書けません。実行時に存在しない値が、move / borrow される値と同じ
括弧に並ぶと、所有権の境界が読めなくなるためです(ADR-0066)。

`<...>` は runtime 引数リスト `(...)` とは別の領域で、次の 2 種類を受け付けます。

- 型パラメータ: 名前だけを書きます。`fn f<T>(value: T) -> T`
- compile-time 値: 名前に型を付けます。`fn sized<n: i64>()`、
  `fn each<worker: Function>(start: i64, end: i64)`

呼び出しはどちらも `<...>` に実引数を並べます。`f<i64>(value)`、`sized<4096>()`、
`std::testing::expect_equal<i64>(expected, actual)`。

compile-time 値として書けるのは整数、`true` / `false`、および `Function`
(top-level function 名)です。型引数推論、generic methods、bounds、
associated types、higher-kinded types、specialization、reflection は実装しません。

通常の function / method 名は、generic かどうかに関係なく snake_case にします。
`<...>` を持つことは PascalCase にする理由にはなりません。型名は PascalCase に
保ちます。例えば `Identity<T>` / `IsI64<T>` ではなく、`identity<T>` /
`is_i64<T>` と書きます。

型と同じ名前の関数は宣言できません(§6.3)。だから storage type の constructor も
型名では綴れません。module 名が型名の snake_case であれば `new`、そうでなければ
関数名で型を名指します。

```kizu
array::new<i64>(allocator)      // std::array の型は Array 1 つ
string::new(allocator)
map::new<[]u8, i64>(allocator)
arena::new<User>(allocator)
mem::box<Item>(allocator, value)  // std::mem は Allocator も持つので型を名指す
```

型名で名付けた module は構築できる型を 1 つだけ持ちます。2 つ目の storage type は
module を分けます。variant は `array::with_capacity<T>(allocator, 64)` のように
横に並べ、`new_` を接頭辞にしません。

`<...>` を type-only 構文として固定しません。将来 fixed-size buffer の長さや
format string など、type 以外の comptime value が必要になった場合は、同じ
`<...>` を static argument list として拡張します。ただし syntax の意味を
予約するだけで、整数や文字列の static argument は受理しません。

Generic function body は未 instantiation のまま top-level runtime code としては検査せず、
明示 static 引数付き call が発生した時に、その static 引数集合で type / ownership /
borrow check します。static 引数は type だけなので、`T` は instantiated body
内で comptime-only の `type` 値として扱います。`type` 値は runtime local、field、
collection element、return value として保持できません。

Std source may define generic wrappers when the type argument is forwarded to an
explicit trusted primitive:

```kizu
// lib/kizu/std/src/array.kizu
pub fn new<T>(allocator: Allocator) -> std::array::Array<T> {
    return std::internal::builtin::array<T>(allocator);
}
```

package 名は、その package の中では import なしに束縛されています。`std::array` は
`std` package の module なので、この file は `std::` で始まる完全パスをそのまま
書けます。

戻り値の型が完全パスなのは、`std::array::Array` が Go 実装の持つ型で、この file が
宣言したものではないからです。compiler が提供する storage type は完全パスでだけ
名前を持ちます。自分の file が宣言した型は短い名前で書けます —— `std::string::String`
は `string.kizu` の `pub struct String` なので、同じ file の中では `String { ... }`
と書きます。

This is not a full monomorphization system. It moves public stdlib
constructor and testing spelling into Kizu source while keeping runtime storage,
test traps, and host interaction as explicit primitive boundaries.

comptime branch:

```kizu
comptime if 1 + 1 == 2 {
    print(sized(comptime 8));
} else {
    print(0);
}
```

`comptime` expression は、整数、真偽値、文字列、compile-time type value、
単項演算、二項演算だけを評価します。`type<i64>` のような `type<T>` literal と、
instantiated generic body 内の static type parameter identifier は `type` 値です。
runtime local value は `comptime` expression から参照できません。

Kizu の canonical spelling は `type<T>` です。`T == i64` のように bare
type name を expression として解決する規則は採用しません。これは value namespace と
type namespace の衝突を避け、`type<[]u8>` や `type<std::map::Map<[]u8, i64>>`
のような複合型でも同じ規則を使うためです。`type` 値は comptime-only であり、
runtime local、field、union payload、collection element、return value として保持できません。

```kizu
fn is_i64<T>(value: T) -> bool {
    comptime if T == type<i64> {
        return true;
    } else {
        return false;
    }
}
```

A `Function` static parameter is a restricted function-name token for std
wrappers that must forward a named function to a trusted primitive. It is not a
closure, cannot capture locals, and cannot be stored in runtime data. It is
declared in `<...>` as `<worker: Function>`, and the argument must be a
top-level function name.

`comptime if` は、コンパイル時に選ばれた branch だけを検査し、lowering します。
これは token stream や AST を書き換える macro ではありません。

## 14. 標準ライブラリ方針

Kizu は将来的に厚めの標準ライブラリを持ちます。

最小 builtin は `print` です。

```text
print
```

`print` は診断用です。値を 1 つ受け取り、改行付きで stdout に書きます。

- `Io` capability を取りません。
- **失敗を報告しません。** 書き込みに失敗しても error を返さず、静かに続行します。
- したがって `!void` ではなく `void` を返し、`try` は要りません。

プログラムの出力としての書き込みは `std::io` を使います。capability を取り、
失敗を `!void` で返します。

```kizu
import std::io;

let handle = io::blocking();
try io::write_stdout(handle, bytes);
```

`print` が受け取れない型は診断になります。黙って何も出さないことはありません。

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

文字列 literal は `[]u8` として扱います。
owned string は primitive ではなく、将来 `std::string::String` で扱います。

C ABI へ `std::string::String` を暗黙に渡してはいけません。
C へ渡す場合は、将来 `std::string::as_c_string` のような明示 API を使います。

安定 allocator factory は `std::mem::page_allocator()` と
`std::mem::fixed_buffer(bytes)` の 2 つです。

```text
std::mem::page_allocator() -> Allocator
std::mem::fixed_buffer(bytes: &var []u8) -> Allocator
```

`Allocator` は visible opaque capability type です。
user-facing `contract` ではなく、field を持つ struct でもなく、
user code が実装できる interface でもありません。
safe Kizu code は `Allocator` 型を名前として使い、local binding に束縛し、
明示 allocator を要求する API に渡せます。

tie のない `Allocator`(`page_allocator()`)は copy 型です。`Array<T>`、
`String`、`Map<K, V>`、`Box<T>`、`std::arena::Arena<T>` の構築に渡しても
allocator binding は move されません。
作られた owner は自身の allocation と `deinit` に必要なものを内部で保持し、
`Allocator` 値そのものに user-visible cleanup method はありません。
allocation が失敗し得る API は `!T` または `!void` で失敗を返します。

`fixed_buffer` は stack buffer(§7.1)の writable view から allocator を
作ります。返る `Allocator` は **tied** で、borrow と同じ扱いです:

* `let` 束縛が必須です。factory 呼び出しを引数位置に inline 書きできません
* tied `Allocator` は copy / alias / escape できません。ただし `&var []u8`
  引数から allocator を作って返す関数は書けます(tie は署名で運ばれ、
  呼び出し側が引数から再導出します)
* allocator が生きている間、元の buffer は exclusive borrow のままです
* tied allocator から作った owner は buffer に tied です: 通常の owner として
  `deinit` を要求されつつ、frame から escape(return、field 格納、move)
  できません。`Allocator` 引数を取る関数へ渡して作らせることはでき、
  その結果は呼び出し側で tie を継承します(§9)
* tied allocator を使う構築・呼び出しの結果は `let` に直接束縛します
* 解放は no-op です。メモリが戻るのは owner の `deinit` ではなく、
  allocator と全 owner の解放後(同じ buffer に新しい `fixed_buffer` を
  作れます)、または buffer の frame 終了です
* buffer 容量の枯渇は `OutOfMemory` です

hidden default allocator、implicit global allocator、missing allocator argument から
`page_allocator()` への fallback は使いません。
safe `std::mem` は raw pointer、allocation method、mutable backing slice、
allocator metadata、deallocation primitive を公開しません。

`std::string::String` は、明示 allocator capability を受け取る
owned byte buffer です。

```text
std::string::new(allocator: Allocator) -> std::string::String
string.append_bytes(bytes: []u8) -> !void
string.append_byte(byte: u8) -> !void
string.reserve(additional: i64) -> !void
string.truncate(length: i64) -> !void
string.len() -> i64
string.capacity() -> i64
string.as_bytes() -> []u8
string.as_mut_bytes() -> &var []u8
string.clear() -> void
string.deinit() -> void
```

`string` primitive は追加しません。
`std::string::new()` のような hidden default allocator は使いません。
`std::string::String` は non-copy / move-only です。
`append_bytes` は source の `[]u8` を move せず、owned buffer に copy します。
`append_byte` は 1 byte を追加します。
`reserve` は少なくとも `additional` byte 分の追加 capacity を確保し、失敗時は `!void` を返します。
`truncate` は length を短くし、capacity は保持します。範囲外の length は `!void` error です。
`capacity` は現在の capacity を `i64` で返します。
capacity の増加戦略は実装が決めます。保証するのは `capacity() >= len()` だけであり、
特定の値に依存するコードは実装を固定してしまうため書けません。
`as_bytes` は owned buffer への local read-only view です。
`as_bytes` の戻り値は local binding に束縛する必要があります。
view が生きている間は `append_bytes`、`append_byte`、`truncate`、`clear`、`deinit` を禁止します。
`as_mut_bytes` は owned buffer への local writable view(`&var []u8`)です。
mutable binding の String からだけ作れ、戻り値は local binding に束縛する
必要があります。view が生きている間、String は exclusive borrow です:
すべての method 呼び出し、`deinit`、共有 view を禁止します。
書き込みは既存 bytes の上書きだけで、length と capacity は変わりません。
`append_bytes`、`append_byte`、`reserve`、`truncate`、`clear` は owned local `String` または
`&var std::string::String` から呼べます。
`clear` は length を 0 にしますが、capacity は保持します。
`deinit` は caller 側の binding を無効化する必要があるため、owned local receiver 限定です。
例外として、owner 型自身の `deinit(self: Owner) -> void` method 内では
`self.field.deinit()` の direct field cleanup を許可します。
UTF-8 validation、C ABI string 変換、raw pointer exposure、
owned bytes 取り出し、String 専用 comparison、String 専用 indexing / slicing は実装しません。
`std::string::String` の public behavior は `lib/kizu/std/src/string.kizu` に実装します。
private `std::array::Array<u8>` storage の上に構成し、safe Kizu に
raw pointer は公開しません。mutable backing は `as_mut_bytes` の
exclusive borrow 経由でだけ公開します(ADR-0096)。public
`std::mem::OwnedBytes` または `std::bytes::Buffer` は、raw storage
provenance の仕様後に検討します。

`std::fmt` は、diagnostic construction 用の最小 formatting API です。
format string、locale、generic display trait、reflection は持ちません。
caller が明示 allocator 付きの `std::string::String` を用意し、
formatting API はその buffer に bytes を append します。

```text
std::fmt::append_i64(out: &var std::string::String, value: i64) -> !void
std::fmt::append_bool(out: &var std::string::String, value: bool) -> !void
std::fmt::append_bytes_literal(
    out: &var std::string::String,
    bytes: []u8,
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
[]T                   contiguous slice value
&[]T                  shared borrowed slice
&var []T              mutable borrowed slice
std::map::Map<K, V>   owned symbol table
std::set::Set<T>      後続 phase
```

`std::mem` は allocation-free な read-only byte helper から始めます。

```text
std::mem::page_allocator() -> Allocator
std::mem::box<T>(allocator: Allocator, value: T) -> !std::mem::Box<T>
std::mem::leak<T>(value: T) -> void
box.borrow() -> &T
box.borrow_mut() -> &var T
box.deinit() -> void
std::mem::len(bytes: []u8) -> i64
std::mem::byte_at(bytes: []u8, index: i64) -> ?u8
std::mem::equal_bytes(left: []u8, right: []u8) -> bool
std::mem::starts_with(bytes: []u8, prefix: []u8) -> bool
std::mem::slice(bytes: []u8, start: i64, end: i64) -> ?[]u8
std::mem::trim_ascii(bytes: []u8) -> []u8
std::mem::bytes_iter(bytes: []u8) -> std::mem::BytesIter
bytes_iter.next() -> ?u8
```

`std::mem::bytes_iter` は iterator protocol(§6.10)の std 綴りです。
`next() -> ?u8` が `while it.next() |byte|` を終端まで駆動し、終端は
失敗ではなく `null` です。cursor は view を capture する struct なので、
歩いている bytes より長生きできません(ADR-0100)。

`std::mem::page_allocator()` は安定 allocator capability factory です。
返された `Allocator` は copy 型であり、複数の owned container や arena の構築に
再利用できます。allocator を受け取る constructor は capability を読み取るだけで、
allocator binding を move しません。

`std::mem::leak<T>(value)` は owner 値を consume しますが解放しません。
leak-on-exit(短命 process が解放を OS に任せる)を source 上に明示する
唯一の手段です。

`std::mem::Limit` は確保上限を明示する union で、`Bytes(i64)` と `Unlimited` を
持ちます。上限を設けない選択も `Unlimited` と綴って source に残します。

`std::mem::Box<T>` は明示 allocator capability で 1 つの owned value を確保する
non-copy / move-only な indirection です。`Box<T>` は struct / union payload に保存できます。
`Box<T>` を含む struct / union は non-copy です。
`borrow` / `borrow_mut` は local borrow source であり、戻り値は local binding に束縛する
必要があります。戻り値の由来は署名から構造的に self に tied と導出されます
(ADR-0098)。borrow field は許可しません。borrow が生きている間は対象
`Box<T>` の move / deinit を禁止します。
`deinit` は owned local `Box<T>` receiver 限定です。
safe API は raw pointer を公開しません。

`std::mem` の safe API は raw pointer を返しません。
`std::mem::slice` と `std::mem::byte_at` は境界外を `null` として返します
(lookup の不在は失敗ではなく答えであるため。基準は `docs/style.md`)。
checked index / slice syntax の実装後は、Kizu std source では
trap-on-bounds-failure の syntax と recoverable な `std::mem` API を用途で使い分けます。
allocator、mutable slice、byte copy / zero / fill は、`std::array::Array<T>` と
mutable slice の仕様後に実装します。

`std::array::Array<T>` は、明示 allocator capability を受け取る
owned contiguous collection です。

```text
std::mem::page_allocator() -> Allocator
std::array::new<T>(allocator: Allocator) -> std::array::Array<T>
array.append(value: T) -> !void
array.len() -> i64
array.capacity() -> i64
array.reserve(additional: i64) -> !void
array.pop() -> ?T
array.pop_or_panic() -> T
array.get(index: i64) -> ?T
array.get_or_panic(index: i64) -> T
array.at(index: i64) -> ?&T
array.at_mut(index: i64) -> ?&var T
array.set(index: i64, value: T) -> !void
array.deinit() -> void
```

`std::array::new<T>()` のような hidden default allocator は使いません。
`array.get` は bounds check し、範囲外なら `null` を返します。
`array.get_or_panic` は testing や invariant-checked code 用の明示 trap variant です。
範囲外なら runtime error で停止するため、recoverable lookup には `get` を使います。
`get` / `get_or_panic` は copy element 限定です。
non-copy element は `at` / `at_mut` で local borrow として読み書きします。
`at` / `at_mut` は borrow optional `?&T` / `?&var T` を返し、範囲内なら
element borrow、範囲外なら `null` です。borrow optional を消費できるのは
capture 条件だけです(`if array.at(i) |elem|` / `while array.at(i) |elem|`)。
capture が element borrow を bind し、その scope の間 array は
borrow されたままです(`at` は shared、`at_mut` は mutable)。
`?&T` を binding に保存する、`orelse` で受ける、関数 signature に書く、の
いずれも拒否します。element borrow が array の変更や解放より長生きする
経路を positional に閉じるためで、`as_bytes` の let-initializer 限定と
同じ整理です。`at_mut` は mutable array binding を要求します。
`while array.at(i) |elem|` は §6.10 の optional 条件 capture そのままなので、
non-copy element の iteration も `get` と同じ形になります。
`pop` は最後の initialized element を array から move して `?T` を返し、
empty array なら `null` を返します。
`pop_or_panic` も最後の initialized element を move して `T` を返し、
empty array なら runtime error で停止します。copy / non-copy のどちらにも使え、
recoverable な empty case を扱う場合は `pop` を使います。
`set` は置換前の element を cleanup してから新しい value を move します。
`deinit` は残っている initialized element を cleanup してから array storage を解放します。
element cleanup は explicit `deinit(self: T) -> void` があればそれを使います。
`T` が owner aggregate で callable な `deinit` を持たない場合、`array.deinit()` の内部に限って
field / payload 内の既知 owner を再帰的に cleanup できます。
これは explicit `array.deinit()` の一部であり、implicit destructor や callable な
`T.deinit()` 合成ではありません。
element borrow が生きている間は `append`、`pop`、`pop_or_panic`、`set`、`deinit` を禁止します。
mutable element borrow が生きている間は array 全体の read も禁止します。
`deinit` 後の array 使用は safe Kizu では禁止します。
`owner.field.deinit()` は owner 型自身の `deinit(self: Owner) -> void` method 内だけ許可し、
その field は同じ body 内で以後使用できません。
`Array<T>` element は arena、handle、nested array、`std::map::Map<K, V>` を
含められます。raw pointer と dyn は入れられません。
この制限は struct field と union payload の中も再帰的に検査します。
これらは provenance と dynamic dispatch の仕様を collection 向けに
固めてから扱います。

`std::map::Map<K, V>` は、symbol table と scope lookup に必要な最小 owned map です。

```text
std::map::new<[]u8, V>(allocator: Allocator) -> std::map::Map<[]u8, V>
map.insert(key: []u8, value: V) -> !void
map.get(key: []u8) -> ?V
map.at(key: []u8) -> ?&V
map.at_mut(key: []u8) -> ?&var V
map.key_at(index: i64) -> ?[]u8
map.contains(key: []u8) -> bool
map.len() -> i64
map.deinit() -> void
```

key type は `[]u8` 限定です。
`insert` は key bytes を owned map 内に copy するため、source key を move しません。
`get` は missing key を `null` として返します(docs/style.md)。
`at` / `at_mut` は value への borrow optional `?&V` / `?&var V` を返し、
key があれば value borrow、なければ `null` です。消費は Array と同じく
capture 条件だけです(`if m.at(key) |v|` / `while m.at(key) |v|`)。
capture の scope の間 map は borrow され(`at` は shared、`at_mut` は
mutable)、shared borrow 中は `insert` / `deinit` が、mutable borrow 中は
すべての map 操作が capture の最終使用まで待ちます。`at_mut` は mutable
map binding を要求します。in-place 更新は
`if m.at_mut(key) |v| { v.* = ...; } else { try m.insert(key, ...); }`
の形で 1 回の lookup になります(ADR-0104)。
`insert` / `get` / `at` / `at_mut` / `contains` は amortized O(1) です。
**map は挿入順で反復します。** 未定義の順序は露出しません。
`key_at` は挿入位置 index の key を返し、末尾を越えたら `null` を返すので、
`while m.key_at(i) |key|` が挿入順の iteration です。key は map storage への
view なので capture 限定で、capture が生きている間 map は共有借用されます
(§7)。
value type は copy type 限定です。
non-copy value、deletion、custom hash/equality は後続で扱います。
`std::map::new<K, V>()` のような hidden default allocator は使いません。
`deinit` 後の map 使用は safe Kizu では禁止します。

`std::testing` は最小 assertion API です。

```text
std::testing::expect(condition: bool) -> void
std::testing::expect_equal<T>(expected: T, actual: T) -> void
std::testing::fail(message: []u8) -> !void
```

`expect` は test assertion 用の void helper です。
condition failure は `std::internal::builtin::test_fail` 経由で runtime error として停止し、
test source は assertion ごとの `try` を書きません。
`fail` は caller-provided `[]u8` を通常の `!void` error として返します。
unreachable branch など、呼び出し側の error-union 経路へ明示的に戻したい場合に使います。
`expect_equal<T>` は明示 static 引数付きの generic assertion です。
failure は `expected ... got ...` 形式の diagnostic を出し、assertion ごとの `try` は不要です。
static 引数が type だけなので、caller は `expect_equal<i64>(1, actual)` のように
期待型を明示します。type argument inference と per-type `expect_equal_i64` family は
導入しません。
test は top-level declaration として書きます。

```kizu
import std::testing;

test "parser accepts minimal function" {
    testing::expect(true);
}
```

`kizu test <path>` は file または package root を受け取り、check 後に
top-level `test` block だけを source order で実行します。`main` は実行しません。
test block は parameterless `!void` body として扱うため、helper が返す `!T` には
`try` を使えます。test block が 0 件なら失敗します。未処理 error がなければ
`test: ok` を表示します。
filesystem-wide test discovery、test filter、test attribute、async test、location-aware
diagnostics、message builder helper は後続で扱います。

## 15. concurrency / async 方針

Kizu は `async fn` / `await` syntax を実装しません。

並行 API も現在は持ちません。`std::task` / `std::channel` / `std::thread` /
`std::sync` / `std::atomic` と `std::io::threaded()` は ADR-0025 で撤回しました。
これらは checker rule だけが存在し、IR lowering も runtime も持たない状態が続いた
ためです。安全規則が実行によって反証されない構造そのものを取り除きました。

**thread は入れます。** 並列処理は Kizu の目標であり、撤回したのは API の形だけです。
順番だけを変えます。実行系が先で、安全規則は動く thread の上でだけ書きます。

戻すときの制約は 2 つです。

* Zig を参照します。hidden global runtime を持たず、`Io` と allocator を明示的に
  渡し、function coloring を作りません(ADR-0039)
* memory race safety は譲りません。Zig は data race を型で防ぎませんが、Kizu は
  防ぎます。safe Kizu で data race を書ける API は採用しません

API の形と個数は未定です。撤回した 8 個の型を一度に戻すことはしません。

### 15.1 Io capability

`Io` capability は残ります。並行 API とは独立した、外部世界に触る権限の表明です。

```kizu
import std::fs;
import std::mem;
import std::string;

fn read_config(io: Io, allocator: Allocator, path: []u8) -> !string::String {
    return fs::read_file(io, allocator, path, mem::Limit::Bytes(1048576));
}
```

方針:

* I/O する関数は `Io` を受け取る
* `Io` を受け取らない関数は local / pure な処理として読める
* hidden global runtime を持たない
* I/O failure は `!T` error として返す

現在の `std::io` implementation:

```text
std::io::blocking()          simple blocking I/O
std::testing::failing_io()   deterministic failing I/O for tests
```

将来の implementation 候補:

```text
std::io::evented()   event-loop or coroutine backed I/O
std::io::uring()     Linux io_uring backend
std::io::kqueue()    kqueue backend
```

`evented` / `uring` / `kqueue` は実装しません。
runtime selection の方針は ADR-0039 に従います。

### 15.2 Io を取る標準 API

`std::fs`:

* `std::fs::read_file(io, allocator, path, limit)` は `!std::string::String` を返す。
  `limit: std::mem::Limit` で確保上限を明示する。超過は
  `std::fs::Error::LimitExceeded` で、`OutOfMemory`(確保失敗)とは分ける
* `std::fs::read_file_into(io, path, out: &var std::string::String)` は `!void` を
  返し、fs 側では確保しない
* `std::fs::write_file(io, path, bytes)` は `!void` を返す
* `std::fs::exists(io, path)` は `!bool` を返す
* `std::fs::metadata(io, path)` は `!std::fs::Metadata` を返す
* `std::fs::read_dir(io, path)` は `!std::array::Array<std::fs::DirEntry>` を返す
* `std::fs::create_dir(io, path)` は `!void` を返す
* `std::fs::remove_dir(io, path)` は `!void` を返す
* `std::fs::remove_file(io, path)` は `!void` を返す
* `std::fs::rename(io, from, to)` は `!void` を返す
* `std::fs::Metadata` は `size: i64` と `is_dir: bool` だけを持つ
* `std::fs::DirEntry` は `name: []u8`、`path: []u8`、`is_dir: bool` だけを持つ
* `path` と `bytes` は caller 側の `[]u8` を保持しない read-only borrow
* I/O failure は `!T` error として返す
* hidden global runtime や暗黙 blocking I/O は使わない
* `std::testing::failing_io()` は deterministic failing I/O として、テストで I/O error path を確認する。
  失敗する implementation は test 用の道具なので `std::io` ではなく `std::testing` が持つ

`std::path`:

* `std::path::join(allocator, left, right)` は `!std::string::String` を返す
* `std::path::clean(allocator, path)` は `!std::string::String` を返す
* `std::path::basename(path: []u8) -> []u8`
* `std::path::dirname(path: []u8) -> []u8`
* `std::path::extension(path: []u8) -> []u8`
* path helper は pure helper であり、filesystem を読まない
* `join` と `clean` は owned buffer を構築するため、allocator を明示し、allocation
  failure を `!T` error として返す

`std::io` / `std::process`:

* `std::io::write_stdout(io, bytes)` は `!void` を返す
* `std::io::write_stderr(io, bytes)` は `!void` を返す
* `std::io::read_stdin(io, allocator, limit)` は `!std::string::String` を返す。
  limit 超過は `std::io::Error::LimitExceeded`
* `std::io::read_stdin_into(io, out: &var std::string::String)` は `!void` を返す
* stdio helper は `Io` capability を必ず要求する
* `std::process::arg_count()` は `i64` を返す
* `std::process::arg(index)` は `![]u8` を返す
* `std::process::env(name)` は `?[]u8` を返し、未設定なら `null` を返す
  (空にしたければ `orelse ""`)
* `std::process::monotonic_millis()` は `i64` を返す
* `std::process::spawn_wait8(argc, arg0, ..., arg7)` は子プロセスを起動して
  終了を待ち、`!i64` を返す。可変長引数を持たないので引数は 8 個までの固定形
* `std::process::exit_code(code)` は `i64` を返す
* `std::process` helper は hidden I/O を持たない

## 16. contract / impl / dyn 方針

Kizu では、Rust trait clone ではない明示的な抽象化として、
`contract`、`impl Contract for Type;`、`dyn` を実装対象にします。

```text
contract                型が満たすべき要求
impl Contract for Type; 型が contract を満たすことの表明(任意)
dyn                     runtime dynamic dispatch を見せる型
```

型は method を持っていれば contract を満たします。宣言は要りません。
`contract` は required method signatures だけを書き、method body も receiver も
書きません。receiver は method 側にあり、contract は形だけを言います。

```kizu
contract Writer {
    fn write(bytes: &Bytes) -> !i64;
}
```

method は receiver 欄で宣言します。書き方は 1 つです。

```kizu
fn (self: &File) write(bytes: &Bytes) -> !i64 {
    return os::write(self.fd, bytes);
}

fn (self: &var File) rename(next: []u8) -> void {
    self.name = next;
}
```

receiver 欄を持つ function はその型の method で、module ではなく型の下に置かれます。
同じ module の 2 つの型が、どちらも `len` を持てます。`impl File { ... }` という
inherent method の囲いはありません。

`impl Contract for Type;` は表明で、書けばその場で検査されます。束縛ではないので、
書かなくても structural に満たせます。書ける場所は型か contract の所有者に
限りません。旧 `satisfy Writer for File` 構文は採用しません。

```kizu
impl Writer for File;
```

structural を採ったので、型の作者が知らない contract を後から満たせます。意図を
書き残したい側は表明を書けます。Go は structural を取り、その結果
`var _ io.Writer = (*File)(nil)` という構文のない定型句が生まれました。表明を
構文として持つのは、その定型句を言語の側に置き直すということです。

`Self` はありません。contract が receiver を書かないので、返り値に自分の型が要る
場合は generic で表します。

`dyn Contract` は dynamic dispatch を型に見せます。

```kizu
fn save(writer: &dyn Writer, bytes: &Bytes) -> !void {
    let n = writer.write(bytes);
    return;
}
```

`dyn` は `&dyn Contract` の動的 dispatch に限定します。
owned dynamic object、generic bounds、最適化された vtable layout は後続 phase で扱います。

## 17. ビルドとキャッシュ

Kizu の toolchain は、キャッシュが無制限に肥大化しない設計にします。

/ 正として扱うコマンド:

```text
kizu parse
kizu check
kizu run
kizu fmt
kizu test
```

`kizu fmt` は現時点では token-based canonical formatter output です。
`--write` / `-w` は file を in-place rewrite します。
完全な source-preserving formatter ではありません。
先頭の連続 `import` block は comment を含まない場合に辞書順へ正規化します。
comment trivia preservation の残りは後続で扱います。
comment trivia preservation までは、`--write` は full-line ではない line comment を含む file を拒否します。
`kizu test` は file または package root 内の top-level `test` block runner です。
`main` 実行、filesystem-wide discovery、filter は行いません。

experimental tooling:

```text
kizu build
kizu ir
kizu cache status
kizu cache prune
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

`kizu test` は top-level `test` block runner として実装済みです。
`kizu lint` は未実装です。

## 18. 実装構成

リポジトリ構成とデータフローは [`docs/architecture.md`](docs/architecture.md) を正とします。

## 19. エラーメッセージ方針

エラーは短く、直接的で、読めるものにします。
詳細な diagnostic message style は
[ADR-0072](docs/adr/0072-diagnostic-message-style.md) に従います。

良い例:

```text
error: moved value `name` was used
  --> examples/move_error.kizu:8:11
```

## 20. 言語の性格

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
