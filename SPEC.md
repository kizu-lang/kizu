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
1 本で、interpreter はありません(ADR-0083)。実装は Go 一本です。
`check: ok` は「checker を通った」ではなく「`run` / `build` が使う lowering が
この program を受理する」の約束です: check は同じ `ir.Lower` を通し、module を
捨てます。

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
arena.at(handle) は arena に tied な `&T` だけを返す
borrow 中の arena の add / deinit、可変 borrow 中の at を許さない
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

self-host は Go の構造に沿って `compiler/` へ移植中で、shipping 実装は Go 一本です。
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
paths = ["src"]
```

`package.name` は user module の root namespace になります。
module path は、`[modules].paths` に指定した source root からの directory path で
決まります。source root 直下が package root module、子 directory が child module
です。同じ directory の production `.kizu` file はすべて同じ module に属し、file
名は module path に影響しません。

```text
src/main.kizu                 -> app
src/cli.kizu                  -> app
src/lexer/lexer.kizu          -> app::lexer
src/parser/ast/ast.kizu       -> app::parser::ast
```

`main.kizu` と `mod.kizu` に特別な module 意味はありません。production file が無い
directory は production module を作りません。したがって library package は package
root module を持たなくてもよく、std の source root 直下には production file を
置きません。

package graph 内で suffix が `_test.kizu` の file は `kizu test <package>` のときだけ、
同じ directory の module に加わります。package を対象にした `run` / `check` / `build`
からは除外します。file path を直接指定した loose-source command は、その1fileを明示的に
選んだものなので通常どおり読みます。
対象 module の private 宣言を見る white-box test はその directory に置きます。依存方向を
逆向きにする integration test は別の test-only directory module に置き、対象の public API
を import します。通常の module cycle 規則を test だけ緩めることはしません。

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
第二実装は持ちません。

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

`main` は `void`、`<E>!void`、または `<E>!std::process::ExitStatus` を返します。
整数は返せません。exit status は platform ごとに形が違い、整数 1 つでは表せない
ためです(ADR-0085)。`std::process::ExitStatus` は compiler が知る std 契約で、
`Success` は 0、`Failure` は 1、`Specific(code)` はその code で終了します。素の
`ExitStatus`(error union でない形)は書けません。error を返した `main` は診断を
出して非ゼロで終了します。

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
    defer values.deinit(allocator);

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
    errdefer values.deinit(allocator);

    try values.append(1);
    return move values;
}
```

で許可する形は cleanup method call の expression statement だけです。

```kizu
defer values.deinit(allocator);
defer text.deinit(allocator);
defer users.deinit(allocator);
```

`defer let ...;`、`defer return ...;`、`defer { ... }`、
`defer defer ...;` は構文として扱いません。
deferred expression は `.deinit(allocator)` のような `void` cleanup call でなければ
なりません。receiver 以外の引数は **`defer` が書かれた場所で読み**、block を出るときに
走るのはその値です。cleanup 対象は自動探索しません。Drop / RAII / implicit destructor
はありません。

deferred cleanup は明示 cleanup call と同じ ownership rule で検査します。
登録時点で receiver を参照できる必要があり、block exit で実行する時点でも
receiver が move 済み、deinit 済み、borrow 中なら拒否します。
`errdefer` の receiver を consume すると、その `errdefer` は退役します。consume 以降の
error exit path では実行しません。move でも明示 `deinit` でも同じで、move を行う
呼び出し自身が失敗する path も含みます。consume 済みの値をそこで解放すると、
同じ値を 2 回解放することになるためです。`defer` / `errdefer` の receiver に別の値を代入することは compile error です。
cleanup は登録時に live だった値を解放するので、名前が別の値を指すようになると、
cleanup は名前が意味しなくなった値を持つことになります。新しい owner は
新しい名前に束縛します。

```kizu
var child = string::new(allocator);
errdefer child.deinit(allocator);
try child.append_byte(cast<u8>(97));  // 失敗したら child を解放する
try parent.append(move child);        // ここから先は parent が child を持つ
try parent.reserve(1);                // 失敗しても child は解放しない
```

`move` marker(§8)が退役する行を指します。義務が place を離れる行と、その
`errdefer` が効かなくなる行は同じ 1 行です。

```kizu
var first = string::new(allocator);
errdefer first.deinit(allocator);     // first だけを覆う
try parent.append(move first);        // 退役

var second = string::new(allocator);  // 新しい owner は新しい名前へ
errdefer second.deinit(allocator);    // second だけを覆う
try parent.reserve(1);                // 失敗したら second を解放する
```

```kizu
var name = string::new(allocator);
errdefer name.deinit(allocator);
if !ok {
    name.deinit(allocator);           // 先に手放してから
    return PlaceError::Rejected;      // error を返す。errdefer は走らない
}
```

退役していない `errdefer` receiver は、その `errdefer` が実行され得る各 error exit path で
borrow されていてはいけません。借用中の値は consume できないためです。
成功 path で owner を move / return することは `errdefer` の実行を要求しません。

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

method receiver path は local binding を root とする field path です。

```kizu
values.len();          // ok: local receiver
self.related.len();    // ok: direct field receiver
self.a.b.len();        // ok: nested field receiver
```

field receiver は root owner の ownership state に従います。read-only method は
owner / path が読めるときだけ、mutating method は owner と重なる path が borrow 中で
ないときだけ呼べます。`field.deinit(allocator)` のような destructive cleanup は、値を保持して
いる場所の direct field 1 段だけ許可します(§8)。borrow の field は拒否します。
`self.a.b.deinit(allocator)` のような nested cleanup は、中間型 `a` 自身の deinit を迂回する
ため拒否します。

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
src/lexer/lexer.kizu                    -> app::lexer                    公開
src/internal/table/table.kizu           -> app::internal::table          app 配下からだけ
src/parser/internal/state/state.kizu    -> app::parser::internal::state  app::parser 配下からだけ
```

`internal` は階層のどこにでも置けます。`X::internal::Y` は `X` とその下の module
からだけ import / 参照できます。部分木の中だけで使う内部 module を、package 全体に
見せずに置けます。

visibility は default private です。同じ directory の file は一つの module を構成する
ため、private な top-level declaration と field はその module の全fileから使えます。
import はfile-localで、各fileが自分の外部依存を明示します。同じmoduleの宣言を使う
ためのimportは不要です。

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

* copy 型(scalar と copy aggregate — §8)の payload は copy として
  束縛され、どの match からでもそのまま使えます。
* copy でない struct / union 型の payload は、owned なローカル値または
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

error union 条件の capture は §11.1 です。

### 6.9.1 bool 演算

Kizu は boolean logic に `and` と `or` を使います。
両辺は `bool` でなければなりません。
`and` と `or` は短絡評価します。

優先順位は低い順に次の通りです。

```text
orelse catch
or
and
== !=
< <= > >=
```

`orelse` と `catch`(§11.1)は同じ段です。

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
arm は `,` で区切ります。最後の arm の `,` は、他の comma-separated list と
同じく省略できます(ADR-0107)。
wildcard pattern `_` を fallback arm として許可します。
`_` arm は最後に 1 つだけ書けます。payload binding はできません。
`_` arm がある場合、明示されていない残りの tag を束ねるため exhaustive とみなします。
`_` arm がない場合は、すべての tag を明示しなければなりません。
expression として使う場合は、すべての arm の value type が一致しなければなりません。
arm value に `;` は付けません。arm の comma、または match を閉じる `}` が
body を終端します。

`match` の arm body は、expression、`return` 文、または block です
(ADR-0093、ADR-0107)。arm の comma、または match を閉じる `}` が
body を終端します。

statement として使う `match` の arm block は文の並びで、他の block と同じく
`let` / 代入 / `defer` / `return` / `break` / `continue` を書けます。
空の block `Tag => {},` はその arm で何もしないことを明示する no-op です。

```kizu
match kind {
    A => {
        count = count + 1;
        print(count);
    },
    B => {},
}
```

expression として使う `match` の arm block は if expression の分岐 block と
同じで、末尾を value で終えます。末尾が `;` で終わる block と空の `{}` は
compile error です。

```kizu
let label = match color {
    Red => {
        let tag = "r";
        tag
    },
    Green => "green",
    Blue => "blue",
};
```

`Tag => return,` は囲む関数からの早期 return です。match の後に実行される文が
ない位置(関数末尾の match と、その直後の `return;` だけが続く形)では結果として
「何もしない」と同じに見えますが、loop 内や後続の文がある位置では関数ごと抜けます。
「何もしない」を意図する arm には `{}` を使ってください。expression として使う
`match` の arm に `return` は書けません。

owner-payload union の deinit 契約(§8)が受理する cleanup arm は
`Kept(payload) => payload.deinit(allocator),` の直接形のままです。block に包んだ
cleanup は契約 error になります。

```kizu
fn (self: Slot) deinit(allocator: Allocator) -> void {
    match self {
        Kept(payload) => payload.deinit(allocator),
        Vacant => {},
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

**関数 pointer 型**は `fn(引数型, ...) -> 戻り値型` と綴ります。値になれるのは
top-level function の名前だけで、closure も捕捉もありません。

```kizu
fn double(value: i64) -> i64 {
    return value * 2;
}

fn apply(f: fn(i64) -> i64, value: i64) -> i64 {
    return f(value);
}

fn main() -> void {
    print(apply(double, 21));
}
```

呼び出しは safe です。指す先はプログラムの生存期間ずっとあり、型が signature を
保証するので、間接であること自体に `unsafe` の対象はありません。指す先が
`unsafe fn` のときは `unsafe fn(...) -> ...` という別の型になり、その呼び出しに
だけ `unsafe` が要ります(§12)。`?fn(...) -> ...` は nullable で、直接呼べません。

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
`Array.at_mut`、`Map.at` / `Map.at_mut`、`Arena.at_mut` の capture 条件と
してだけ存在します。capture が payload borrow そのものになり、その scope の
間 container は borrow されます。receiver は local binding のほか、owner からの
field path(`owner.field.at_mut(...)`)を書けます: このとき borrow は owner の
該当 path に付き、owner の move とその path に重なる操作が capture の最終使用
まで待ちます。保存と `orelse` は拒否します(§14.4、§10)。

関数は bare の `?&T` / `?&var T` を戻り値型として宣言できます。契約は
署名から構造的に導出され(ADR-0098)、呼び出し側の capture は borrow を
運べる引数(receiver 含む)を保守的に borrow します。body が返せるのは
borrowed parameter 由来の borrow だけで、local な container の borrow を
返す経路は拒否します。宣言した borrow-optional return は capture 条件と
並ぶ第二の消費位置で、それ以外の場所に `?&T` の値は存在できません。

```kizu
fn (self: &var Registry) user(id: i64) -> ?&var User {
    return self.users.at_mut(id);
}

if registry.user(id) |u| { u.visits = u.visits + 1; }
```

現在の制限:

* `??T` / `?!T` は書けない(optional を包めるのは error union だけ)。
  generic 実体化が作る綴り(`Array<!i64>` の `pop()` が返す `?!i64`)も
  同じ規則で拒否される
* struct field に置けるのは plain copy data、arena handle、owner の optional
  (`?i64`、`?arena::Handle<T>`、`?std::string::String` など)。view を包んだ
  optional(`?[]u8`)は field に置けない —— view の義務は借用で、それを開く
  capture が field 型を読む規則から見えないため。union payload・static
  argument(`Array<?u8>` など)・borrow(`&?T`)の対象にはできない
* `?Owner` field の cleanup 契約は §14.4 にある。`deinit` の中で optional を開いて
  中身を解放する。宣言しなければ、それを行う body が導出される
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
copy 型は複製しても cleanup 義務を生まないと構造から証明できる型で、
代入・値渡し・コレクションからの読み出しで複製され、
元の binding は使い続けられます。

copy 型:

```text
bool / void
i8 / i16 / i32 / i64 / u8 / u16 / u32 / u64 / usize / isize
f32 / f64
enum / error set
std::arena::Handle<T>
copy aggregate
```

`std::arena::Handle<T>` は arena 内の値を指す opaque な ID で、値を
所有するのは arena です。ID の複製は解放責務を生まないため copy です
(§10)。

copy aggregate は、scalar・enum・error set・arena handle・copy aggregate
だけを field / payload に持つ struct / union です(#1597)。copy 判定は型の構造から
導出され、注釈はありません。ただし明示 `deinit` を宣言した型は、
その宣言が cleanup contract なので全 field が copy でも move-only に留まります。
`[]u8` を transitively 含む struct は copy ではなく view の規則
(§9, ADR-0100)に従います。この一覧の他に `[]u8`(§7)、
tie のない `Allocator`(§14)、raw pointer(§12)が copy です。

上のどれでもない型の値は所有され、代入と値渡しで move されます:

```text
std::array::Array<T>
std::map::Map<K, V>
std::string::String
std::mem::Box<T>
arena-owned value
owner aggregate と、明示 deinit を宣言した struct / union
```

木や入れ子のような再帰的データ構造は、子を `std::mem::Box<T>` の
struct / union payload として直接持つ形でも、要素を
`std::arena::Arena<T>` に置き、子を `?arena::Handle<T>` field で
参照する平坦な形でも表せます(§7 optional field、§10)。

owner field または owner payload を含む struct / union は owner aggregate です。
owner aggregate は copy できず、値渡しや代入では move されます。
block を出る時点で、owner 値は次のいずれかで consume 済みでなければ
compile error です(ADR-0091)。

* `value.deinit(allocator)` または `defer value.deinit(allocator);` による明示 cleanup
* 別の owner aggregate / container への move
* owned return value として caller への move
* `std::mem::leak(value)` による明示 leak

名前の付いた place から owner が出る move には `move` marker を書きます。

```kizu
try parent.append(move child);        // 引数
return Bag { values: move values };   // struct / union literal の field
return move name;                     // return
let held = move name;                 // 束縛
```

marker が付くのは place から値が出る位置だけです。call の結果や literal のような
temporary は壊す place を持たないので付けません。method の receiver にも付けません
—— receiver を consume するのは `deinit` の契約で、その語が既に見えています。
copy 型の place に書くのは compile error です。手放していないためです。

marker は義務が place を離れる行を指します。そこは `errdefer` が退役する行でも
あり(§6.3)、退役が source に現れる唯一の場所です。

generic な body は instantiation ごとに同じ 1 行を持ちます。owner の instantiation
が手放す以上 marker は必要なので、copy の instantiation はそれを受け入れます。
片方を満たす綴りがもう片方を破ると、その関数が書けなくなるためです。

consume は path ごとに決まります。分岐の一方でだけ consume する owner は
compile error です。consume しなかった path はその値を解放できず、合流後に
解放すると consume 済みの path で二重解放になるためです。同じ理由で、loop 本体は
外側で宣言された owner を consume できません。body の実行回数は不定で、0 回なら
未解放、2 回以上なら二重解放です。loop が consume してよいのは loop 自身が
作った値、つまり `|name|` capture が束縛する値です。

**同じ規則が owner field にも適用されます。**`deinit` が field を解放するのは
1 つの path で起きる出来事なので、値と同じ 3 つの制約を受けます。

* 分岐の一方でだけ、あるいは match の一部の arm でだけ field を解放するのは
  compile error です(`match` は、その先へ続く arm すべてで揃える必要が
  あります)
* loop 本体で外側の値の field を解放するのは compile error です
* `deinit` は receiver の owner field をすべて consume します。これは関数の
  末尾だけでなく、**関数を離れるすべての path** で成り立ちます。早期 `return`
  も同じです

owner field への代入も compile error です。代入は置き換え前の値を解放しないので
leak します(owner 要素の `set` と同じ理由)。

owner を produce する式を、値を束縛せずに文として書くことも compile error です。
consume の義務は束縛に付くので、束縛しない値は義務を負う先を持ちません。
`?T` / `E!T` に包まれていても同じです —— `array.pop();` は要素を container の外へ
出したうえで捨てます。意図的に捨てる場合も `let _ = ...` で束縛し、
上の consume のいずれかを書きます。

owner aggregate を値引数として受け取る関数は、その値を consume する義務を負います。
これは失敗 path も含みます。error を返す path でも、受け取った以上はそこで
consume しなければなりません。呼び出し側は move 済みで、その値にもう触れない
ためです。`std::array::Array.append` と `std::mem::box` は確保に失敗すると値を
格納しませんが、格納しなかった値をその場で解放してから error を返します。
読み取りだけを行う関数は `&T` で受け取ります。
mutation が必要な関数は `&var T` で受け取り、consume する関数は owner aggregate を値で受け取ります。

owner field または owner payload を含む型は、それを持つことによって owner です。
`deinit(self: T, allocator: Allocator) -> void` を宣言しなければ、保持しているものを
宣言順に consume する body が導出されます。

```kizu
struct Visitor {
    name: string::String,
    nick: ?string::String,
}
// 導出される body:
//   self.name.deinit(allocator);
//   if self.nick |held| { held.deinit(allocator); }
```

義務が field の義務だけである型に、書ける body は 1 つしかありません。「`deinit` は
receiver の owner field をすべての path で consume する」がそれを固定しており、
field は互いに alias しないので順序も効きません。手で書いても書けるのは導出結果
だけで、それは原理 10 が畳めと言う定型です。cleanup contract は field の型に既に
見えており、呼び出し `value.deinit(allocator)` は source に残るので、原理 2 の hidden control
flow にも当たりません。

自分で確保したものを解放する型 —— allocator から取ったメモリ、descriptor ——
は `deinit` を宣言します。その義務は型のものであり、どの field のものでもないため、
導出できません。宣言した型はその body を使います。

owner field は、値を保持している場所で 1 つずつ consume できます。値で受けた
parameter の呼び出し側は既に手放しており、local は frame 自身のものなので、
分解しても二重解放になりません。義務が aggregate から field へ移るだけで、
どの field も block を出るまでに consume されなければなりません。

```kizu
fn finish(partial: Partial) -> Full {
    return Full { first: move partial.first, last: move partial.last };
}
```

`deinit` を宣言した型は丸ごと consume します。その義務は型のものであり、
どの field の consume でも果たされないため、分解すると出口の無い値が残ります。
例外はその型自身の `deinit` body で、そこは宣言義務を果たしている最中です。

field を 1 つ取り出した値は、もう自分の型と一致しません。丸ごと move すること、
borrow すること、`deinit(allocator)` を呼ぶことは compile error です。いずれも既に無い
field に手を伸ばします。残りも取り出すか、1 つも取り出さないかです。

borrow から field を取り出すことはできません。貸し手がまだ持っているものを
解放することになります。

cleanup の名前は `deinit` 1 つです。`deinit` は値と、値が保持しているものを
解放します。container なら要素を要素自身の `deinit(allocator)` で consume してから buffer を
解放し、何も保持しない要素ではその consume が空になるだけです。要素型が決まって
いない generic code も同じ 1 つを書きます。名前が 1 つなので任意の深さに合成でき、
owner 要素の container を要素にする入れ子も書けます。owner 要素の `set` は、置き換え
前の要素を leak するため型 error です。Arena も owner 要素を持てて、arena の
storage を解放する前に各 initialized element を consume します。

owner payload を持つ `union` も owner aggregate です。宣言しなければ、active variant の
payload を consume する `match` が導出されます。その `deinit` は active variant の
payload だけを、通常は exhaustive な `match` で cleanup します。inactive variant の
payload storage は cleanup しません。tag が初期化済みと示す payload だけを処理します。
inline payload の size と alignment は compile time に確定している必要があります。

```kizu
union MirStmt {
    LetCall(MirLetCall),
    ReturnExpr(MirReturnExpr),
    If(MirIf),
}

fn (self: MirStmt) deinit(allocator: Allocator) -> void {
    match self {
        LetCall(stmt) => stmt.deinit(allocator),
        ReturnExpr(stmt) => stmt.deinit(allocator),
        If(stmt) => stmt.deinit(allocator),
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
* method call の `&var` receiver は two-phase: 引数の評価中は共有借用として
  予約し、引数がすべて確定した時点で排他化する。引数は receiver を読めるが、
  receiver を borrow / move する引数は拒否する(ADR-0106)
* `&user.name` や `&user.profile.name` のような field path borrow を許可する
* field borrow 中でも disjoint な path への assignment は許可する
* field borrow 中の owner 全体の move と、borrow 中の path に重なる path への
  assignment / borrow は禁止する。path の重なりは一方が他方を含むこと:
  `user.profile.name` は `user.profile` と重なり、`user.age` とは重ならない
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
* **view を持てる struct** も tie を運びます(ADR-0100)。field を
  transitively 辿って `[]u8` を含む型がこれに当たり、別の field が owner
  でも同じです。この型を返す関数の戻り値は、borrow-class な
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

owner field を持たない捕捉 binding は borrow class に入ります。owner field
を持つ捕捉 binding は通常の owner のまま source への tie も持ち、明示
`deinit` 義務を失いません。どちらも frame から escape できず(return、move、
struct への再格納は拒否)、source は binding の最後の使用まで borrow 中です。
owner の通常の最後の使用は、その義務を消費する `deinit` です。
borrow-class 値の `[]u8` field 読みは let では同じ tie を継ぎ、move 文脈では
escape として拒否します。source が関数 parameter だけの捕捉は自由な値の
ままです: parameter は frame より長生きし、呼び出し側が署名から tie を
再導出します。

契約は署名だけから導出され、body は参照されません。
名前付き lifetime parameter、lifetime bounds、anonymous lifetime は
採用しません。borrow field(`&T` の field 保存)は採用せず、view の
struct 捕捉は上記の view-capture 規則が担います。

safe borrow binding は通常の field access 構文で field を読めます。
`&var T` binding は通常の field assignment 構文で field を更新でき、
binding そのものへの assignment は borrow の指す caller の storage へ
store します。`n = v` と `n.* = v` は同じ場所への store です。ただし
T が deinit 義務を持つ型の場合、caller の生きた所有値を黙って落とす
ことになるため拒否します(所有者だけが値を消費できます)。

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
`&var` parameter が無いこと(`&var []u8` は差し替え不可のため除外)。method
でも同じ規則で、`&var self` の receiver も「view を保持できる型の `&var`
parameter」として数えます。貸与は呼び出し statement の終了で終わります。view を捕捉し得る struct を返す
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
* `&T` binding への assignment は禁止
* `&var T` binding への assignment は許可(caller の storage へ store。
  T が deinit 義務を持つ型は拒否)
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
var users = arena::new<User>(allocator);
let alice = users.add(allocator, User { name: "alice" });
print(users.at(alice).name);
```

`std::arena::Arena<T>` は複数の `T` を所有します。
core arena の構築は明示 allocator capability を要求し、
`std::arena::new<T>()` は無効です。allocator 引数は読み取りとして扱われ、move されません。
`arena::new` は header そのものを作るだけで何も確保しないので、失敗しません。
storage を買うのは最初の `add` で、失敗を言うのもそこです。
allocator が断るのは壊れたプログラムではなく(`mem::fixed_buffer` は使い切るのが
普通の動作です)、`add` は `std::mem::Error!` で返します。

`std::arena::Handle<T>` はポインタではありません。arena 内の値を指す opaque な ID です。
値を所有するのは arena なので、handle は copy 型です(§8)。
handle は自分を作った arena instance を運びます。だから field を経由しても
container に入れても関数境界を越えても出自は消えず、別の arena に渡した handle は
読み取りが拒否します(ADR-0134)。

ルール:

* `std::arena::new<T>(allocator)` は `Allocator` を明示して `std::arena::Arena<T>` を作る。
  header そのものを作るだけで何も確保しない
* `std::arena::Arena<T>.add(allocator, value)` は value を arena に move する。
  storage を買う call なので allocator を名指す(§14.3)
* `std::arena::Arena<T>.add(allocator, value)` は
  `std::mem::Error!std::arena::Handle<T>` を返す。storage を買う call なので、
  allocator が断ったことを言うのもここ(§11)
* `std::arena::Arena<T>.at(handle)` は arena に tied な shared borrow `&T` を返す。
  直接 field / method / match を読め、local binding に束縛した場合は最後の使用まで
  arena を borrow する。その間は `add` / `deinit` を実行できず、要素を move
  できない。borrow parameter を根に持つ arena からは返せるが、local arena 由来の
  borrow は function から escape できない
* `std::arena::Arena<T>.at_mut(handle)` は borrow optional `?&var T` を返す
* `std::arena::Arena<T>.deinit(allocator)` は initialized element を各要素の
  `deinit(allocator)` で consume してから arena storage を解放し、binding を無効化する
* `std::arena::Arena<T>.deinit(allocator)` は owned local receiver の呼び出しだけを許可する
* `owner.field.deinit(allocator)` は値を保持している場所の direct field だけ許可する(§8)
* handle は copy 型で、代入・値渡し・格納しても元の binding は使い続けられる。
  複製は元と同じ arena 由来を引き継ぎ、以下の規則は複製にも適用される
* handle は borrow より長生きしてよい
* handle は対応する arena より長生きしてはいけない
* `deinit` 後の arena と、その arena 由来の既知 handle は使用してはいけない
* handle は raw pointer ではない
* arena からの削除は実装しない
* 1 つの arena が保持できる要素は 2^32 - 1 個まで。これを越える `add` は
  runtime error で停止する。handle が自分の arena instance を運ぶための
  上限で、越えると別 instance の handle が区別できなくなる(ADR-0134)
* `?arena::Handle<T>` は struct field に置ける(§7)。「子が無い」を
  番兵値でなく型で表し、再帰的データ構造は arena + optional handle の
  平坦な形で書く
* handle と arena の取り違えは、両者の由来が compile 時に判明していれば
  その場で拒否する。borrow で受けた arena や field から読んだ handle は
  由来不明として型検査を通るが、**読み取りが実行時に拒否する** ——
  handle は自分を作った arena instance を値として運ぶので、別の arena に
  渡した handle は `at` では runtime error で停止し、`at_mut` では null を
  返す(ADR-0134)。同じ `arena::new` を 2 回実行して作った 2 本も別 instance
  であり、互いの handle は通らない
* 署名に借用 `Arena<T>` 引数と値渡し `Handle<T>` 引数が同じ `T` で
  1 つずつだけ現れる場合、caller はその組を「この handle はこの arena の
  もの」という契約として署名から導出し、call site で両引数の由来が判明して
  いれば一致を検査する。片方でも由来不明なら通り、契約は次の署名に連鎖する。
  同じ `T` の arena 引数が複数あるなど組が曖昧な署名からは導出しない

`at_mut` は handle の指す値への borrow optional `?&var T` を返し、capture
条件だけが消費できます(§7)。`at_mut` は mutable な受け手(`var` binding
または `&var` 借用)を要求し、
capture の scope の間 arena は mutable に borrow され、`at` / `add` /
`deinit` は capture の最終使用まで待ちます(`add` は realloc で element
storage を動かします)。

```kizu
if users.at_mut(alice) |u| {
    u.visits = u.visits + 1;
}
```

handle の由来は静的 provenance 検査が保証するため、capture の else 分岐が
受けるのは検査をすり抜けた別 arena handle の残余だけで、通常の経路では
到達しません。

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

### 11.1 catch

`try` が伝播、`catch` が処理です。`catch` は set を宣言した error union
`E!T` を call site で処理します。`!T` は set を持たず member を列挙できない
ので、伝播専用のままです。

expression 形は `orelse`(§6.9.1)の error union 版です。成功なら `T` の
値、失敗なら右辺を返します。右辺は失敗したときだけ評価されます。
expression 形で error 値は束縛できません。

```kizu
let port = read_port(false) catch 8080;
```

右辺には `orelse` と同じ guard 形(`return [expr]` / `break [:label]` /
`continue [:label]`)を書けます。優先順位は `orelse` と同じ段です(§6.9.1)。

```kizu
let port = read_port(false) catch return -1;
```

error 値に触るのは statement 形だけです。optional の capture(§6.9)の
error union 版で、`else |err|` が error member を束縛します。`err` の型は
`E` で、enum と同じ規則の `match` で分岐します(§11.2)。

```kizu
if read_port(true) |port| {
    print(port);
} else |err| {
    match err {
        NotFound => print(0),
        InvalidPort => print(-1),
    }
}
```

ルール:

* `catch` / error capture の対象は `E!T` だけです。`!T` は `try` でしか
  消費できません
* 条件が error union の `if` は `else |err|` が必須です。書かなければ失敗を
  黙って捨てる形になるためです
* `T` が `void` の場合、成功 capture は書きません: `if f() { } else |err| { }`
* 成功 payload の階級は optional capture と同じ規則です(§7)。owner payload
  は capture / `catch` 式の結果として消費します
* `catch` / `else |err|` で処理した error は関数を出ません。error return
  path ではないので、`errdefer`(§6.3.1)は実行しません

### 11.2 error set

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
* 宣言した set は `match` で網羅的に分岐できる(§11.1 の capture で束縛した
  値)。`!T` は set を持たないので分岐できない
* error 値が `main` から出た場合、`runtime error: Name::A` として報告される

set は他の set の和として宣言できます。

```kizu
error JsonError {
    Truncated,
}

error CacheError = FsError or JsonError;
```

`=` 形は member 集合の和です。新しい error 値は作らず、合成された member は
元の set の値そのものです。`FsError::NotFound` は変換なしで `CacheError` の
member でもあるので、`CacheError!T` の関数は `FsError!T` の呼び出しを
`try` で伝播できます。

* 値を宣言するのは `{ }` 形だけです。`=` 形の右辺は宣言済み set を `or` で
  つないだ列で、新 member は書けません。自前の member も足したい場合は、
  その member を持つ set を宣言して和に入れます
* 右辺に set を 1 つだけ書くと、その set の別名になります
* 和の和は単に和で、階層を作りません。同じ member が複数の経路から来ても
  1 つに数えます
* member の名前は出自に残ります。参照は元の set で書き、合成側の名前
  (`CacheError::NotFound`)は作りません
* `match` の arm は bare 名、または元 set 修飾名で書きます。合成が同名
  member を複数の set から受け取る場合、bare 名の arm は compile error で、
  元 set を修飾して書きます

ルール:

* `try` は `!T` を返す関数内でだけ使える
* `try` の operand は `!T` でなければならない
* `E!T` の `E` は宣言済みの `error` set でなければならない
* `E!T` では `E` の member または `T` を返せる
* `!T` は set を宣言しないので、body はどの set の member でも伝播・返却できる
* `E!T` と宣言した場合、`try` で伝播できるのは member 集合が `E` の部分集合で
  ある set(`E` 自身と、その合成元)だけ
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

`unsafe struct` は field を `pub` にできません。通常のprivate fieldは同じmoduleの
全fileから使えますが、`unsafe struct` の構築とfieldへの書き込みは、その宣言がある
fileの中だけに閉じます。別fileでは `unsafe` を付けても拒否します。これにより、
不変条件を作り変えられる監査範囲はmoduleの大きさにかかわらず1fileに固定されます。

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
* `fn(...) -> T` を通した呼び出しに `unsafe` は要らない
* `unsafe fn(...) -> T` を通した呼び出しには `unsafe` が要る
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

compile-time 値として書けるのは整数、`true` / `false`、`Function`
(top-level function 名)、および `Field`(struct の public field 名)です。型引数推論、generic methods、bounds、
associated types、higher-kinded types、specialization は実装しません。
reflection は §13.1 の comptime 専用 structural reflection だけを持ち、
runtime reflection と AST 書き換えは持ちません。

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
borrow check します。同じ関数と同じ static 引数の組は 1 回だけ検査します。
instantiation の入れ子は 32 段までで、超えると診断になります。呼ぶたびに
static 引数が育つ body は同じ組に戻らないため、上限がなければ検査が止まりません。static 引数は type だけなので、`T` は instantiated body
内で comptime-only の `type` 値として扱います。`type` 値は runtime local、field、
collection element、return value として保持できません。

Std source may define generic wrappers when the type argument is forwarded to an
explicit trusted primitive:

```kizu
// lib/kizu/std/src/array/array.kizu
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
単項演算、二項演算、および §13.1 の `std::meta` 述語だけを評価します。
`type<i64>` のような `type<T>` literal と、instantiated generic body 内の
static type parameter identifier は `type` 値です。
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

A `Field` static parameter is one public field of a struct. It is declared in
`<...>` as `<f: Field>`, and the argument must be the source name of a public
field of the type argument written before it. The body reads the field through
the `std::meta` forms (`field_name<T, f>`, `field_type<T, f>`, `field<T, f>`),
so the function is checked and lowered once per field, the way a `comptime for`
expansion is.

`Function` and `Field` name compile-time tokens, not types values can have.
Neither can be stored in a struct field, returned, or written in `(...)`, and
wrapping one (`?Field`, `[]Function`) does not change that.

`comptime if` は、コンパイル時に選ばれた branch だけを検査し、lowering します。
これは token stream や AST を書き換える macro ではありません。

### 13.1 comptime for と structural reflection

`comptime for` は compile-time list の反復です。綴りは runtime の `for`
(§6.11)と同じ capture 構文です。

```kizu
comptime for std::meta::public_fields<T>() |f| {
    print(std::meta::field_name<T, f>());
}
```

`comptime if` と同じく、これは token stream や AST を書き換える macro では
ありません。展開された各反復を、その束縛のもとで型・所有権・borrow 検査します。

反復できるのは `std::meta::public_fields<T>()` と `std::meta::variants<T>()`
だけです。整数 range は runtime の `for` が持ちます。

`std::meta` は、struct と sum type の構造をコンパイル時に読むための組み込みの
式の形です。comptime 専用の**型**は持ちません(ADR-0113)。

```text
std::meta::is_struct<T>()          -> bool    comptime-only
std::meta::is_enum<T>()            -> bool    comptime-only
std::meta::is_union<T>()           -> bool    comptime-only
std::meta::is_optional<T>()        -> bool    comptime-only
std::meta::is_array<T>()           -> bool    comptime-only
std::meta::is_box<T>()             -> bool    comptime-only
std::meta::is_map<T>()             -> bool    comptime-only
std::meta::is_owner<T>()           -> bool    comptime-only
std::meta::release_names_allocator<T>() -> bool comptime-only
std::meta::has_public_fields<T>()  -> bool    comptime-only
std::meta::element<T>                         comptime-only、型の位置に書く
std::meta::public_fields<T>()                 comptime-only list、comptime for 専用
std::meta::field_name<T, f>()      -> []u8    comptime-only
std::meta::field_type<T, f>                   comptime-only、型の位置に書く
std::meta::field<T, f>(value: &T)  -> &F
std::meta::construct<T, worker>(args...) -> !T
std::meta::variants<T>()                      comptime-only list、comptime for 専用
std::meta::variant_name<T, v>()    -> []u8    comptime-only
std::meta::variant_type<T, v>                 comptime-only、型の位置に書く
std::meta::has_payload<T, v>()     -> bool    comptime-only
std::meta::variant<T, v>(payload)  -> T
std::meta::unsupported<T>()                   compile error にする
```

`construct<T, worker>(args...)` は `T` の public field を宣言順に組み立てて
`T` を返します。各 field の値は `worker<T, f>(args...)` の戻り値で、runtime 引数は
全 field に同じものが渡ります。field ごとの違いは worker が `field_name<T, f>` と
`field_type<T, f>` から読みます。

```kizu
// construct<Names, make_field>(allocator) が表すコード
let first = try make_field<Names, first>(allocator);
errdefer first.deinit(allocator);
let second = try make_field<Names, second>(allocator);
Names { first: move first, second: move second }
```

`errdefer` は owner field にだけ並びます。値は struct literal が一度に取るまで
別々の binding なので、半端に組んだ `T` が置かれる場所はありません。owner field を
持つ `T` では第 1 引数が `Allocator` でなければなりません —— worker はそこから
field を作り、`errdefer` はその同じ allocator を名指して解放します(§15.3)。worker の
戻り値型はその field の型でなければならず、public field を 1 つも持たない型は
compile error です(値の行き先が無いため)。

`is_owner<T>()` は、その型の値が deinit 契約を持つかを答えます。`T` を保持する
generic code が「解放するとは中身も解放することか」を問う唯一の手段で、答えは
checker が使うものと同じです。

`release_names_allocator<T>()` は、その解放が allocator を名指すか
—— `deinit` が allocator を取るか —— を答えます(ADR-0132)。memory を解放する
owner と descriptor を閉じる owner は同じ引数を取らないので、要素を解放する
container はこれを聞いてから呼びます。宣言された `deinit` の parameter list が
答えで、宣言していない owner は derived deinit が allocator を渡すので true です。

`unsupported<T>()` は、その型を扱う case が無いことを compile error にします。
`comptime if` は選ばれた branch だけを検査するので、最後の else に書けば、
扱えない型が来たときにだけ error になります。診断は型と、拒否した関数を
名指しします。閉じた集合を歩く walk が、集合の外を黙って通さないための形です。

`is_*` は `comptime if` の条件に書けます。`comptime` expression はこれらの
組み込み形も評価します。述語が答える型の種類は std が持つ container で
閉じています。ユーザーは generic 型を宣言できない(§7)ので、この集合の外に
新しい種類は現れません。

`field_name` の値は source の field 名を持つ `[]u8` literal です。static
storage を指し、確保は起きません。

`element<T>` は `?T`、`std::array::Array<T>`、`std::mem::Box<T>` の中身の型を
返します。`std::map::Map<K, V>` では、key は entry の見つけ方なので、保持する
値の型 `V` を返します。`field_type<T, f>` はその field の型を返します。どちらも
型の位置に書くので、型値として比べるときは `type<std::meta::element<T>>` と
綴ります。static 引数にも書けるので、そのまま再帰できます。

```kizu
fn encode_value<T>(encoder: &var std::json::Encoder, value: &T) -> !void {
    comptime if std::meta::is_struct<T>() {
        try encoder.begin_object();
        comptime for std::meta::public_fields<T>() |f| {
            try encoder.write_key(std::meta::field_name<T, f>());
            try encode_value<std::meta::field_type<T, f>>(
                encoder,
                std::meta::field<T, f>(value),
            );
        }
        try encoder.end_object();
    }
    return;
}
```

`std::meta::field<T, f>(value)` は `&value.<f の名前>` と同じもの、つまり §9 の
field path borrow です。借用の追跡、衝突判定、`&var` の排他は field path borrow の
規則がそのまま適用されます。provenance は署名から構造導出します(§9)。

capture 束縛(上の `f`)は値ではありません。書ける位置は `std::meta::*` の
static 引数だけです。

* `let g = f;` のように binding へ束縛できません
* 関数や method の引数として渡せません
* 比較・演算の対象にできません
* runtime local、field、union payload、collection element、return value として
  保持できません

列挙するのは struct の `pub` field、enum の tag、union の variant で、順序は
どれも source の宣言順です。

### 13.2 comptime match

`comptime match` は、値が今どの variant かで分岐する compile-time 展開です。
`comptime for` が field を歩くのに対し、これは variant を歩きます。

```kizu
comptime match value |v, payload| {
    comptime if std::meta::has_payload<T, v>() {
        print(std::meta::variant_name<T, v>());
        print(payload);
    } else {
        print(std::meta::variant_name<T, v>());
    }
}
```

展開されるのは `match` そのものです —— variant ごとに 1 arm、宣言順、payload を
持つ variant では第 2 capture がその arm の payload binding になります。
exhaustiveness も payload の借用も所有権も、§6.12 の `match` の規則がそのまま
適用されます。新しい分岐機構ではありません。

```kizu
// 上が表すコード(union Shape { Point, Circle(i64) } の場合)
match value {
    Point => { ... },
    Circle(payload) => { ... },
}
```

第 1 capture(上の `v`)は `comptime for` の capture と同じ compile-time
token で、書ける位置は `std::meta::*` の static 引数だけです。第 2 capture は
payload を持つ variant の展開でだけ束縛されます。payload を持たない variant の
展開でそれを書くと未定義の名前になるので、`has_payload<T, v>()` で分けます。

payload を 1 つも持たない型 —— つまり enum —— では第 2 capture を省けます。

```kizu
comptime match color |v| {
    print(std::meta::variant_name<T, v>());
}
```

`std::meta::variant<T, v>(payload)` は `T::<v の名前>(payload)` と同じもので、
payload を持たない variant では `T::<v の名前>` です。walk が値を**作る**側で
arm を名指しする唯一の手段で、arm は呼び出し側が型として書けないためです。

## 14. std とのインターフェース

言語が std について持つ契約です。**compiler が知っていること**だけを書きます:
capability としての `Allocator`、std storage 型に対する borrow / ownership の
検査規則、`test` 宣言。各 module が公開する API とその振る舞いは
`docs/std/` にあります。

境界は 1 問で決まります —— 利用者が自分で同じものを書けるか。書けるなら
`docs/std/`、書けないならここです。

### 14.1 print

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

### 14.2 module 名と文字列

stdlib module は lowercase namespace names にします(`std::string`、
`std::array`、`std::json`)。

文字列 literal は `[]u8` として扱います。owned string は primitive ではなく
`std::string::String` です。

C ABI へ `std::string::String` を暗黙に渡してはいけません。C へ渡す場合は、
将来 `std::string::as_c_string` のような明示 API を使います。

### 14.3 Allocator capability

安定 allocator factory は `std::mem::page_allocator()`、
`std::mem::fixed_buffer(bytes)`、`std::mem::allocator_from(...)` の 3 つです。

```text
std::mem::page_allocator() -> Allocator
std::mem::fixed_buffer(bytes: &var []u8) -> Allocator
std::mem::allocator_from<T>(
    state: &var T,
    alloc: unsafe fn(&var T, i64) -> ?ptr<u8>,
    free:  unsafe fn(&var T, ptr<u8>, i64) -> void,
) -> Allocator
```

`Allocator` は visible opaque capability type です。
user-facing `contract` ではなく、field を持つ struct でもありません。
safe Kizu code は `Allocator` 型を名前として使い、local binding に束縛し、
明示 allocator を要求する API に渡せます。

`allocator_from` は user が書いた確保・解放から allocator を作ります
(ADR-0129)。`alloc` は要求された byte 数の領域を返すか、確保できなければ
null を返します。`free` は `alloc` が返した pointer と、その確保に渡された
size を受け取ります。2 関数は `unsafe fn` で、契約 —— 返した領域は要求 byte
数だけ書き込め、`free` に戻すまで他へ配られない —— は書いた側が負います
(§12)。返る `Allocator` は `state` に **tied** で、下の `fixed_buffer` と
同じ規則に従います。

tie のない `Allocator`(`page_allocator()`)は copy 型です。`Array<T>`、
`String`、`Map<K, V>`、`Box<T>`、`std::arena::Arena<T>` の構築に渡しても
allocator binding は move されません。
`Allocator` 値そのものに user-visible cleanup method はありません。
allocation が失敗し得る API は `!T` または `!void` で失敗を返します。

**確保も解放も allocator を名指します。** owner の `deinit` は receiver と
`Allocator` の 2 つを取り、storage を要求し得る method —— `Array.append` /
`append_bytes` / `reserve`、`String.append_bytes` / `append_byte` /
`append_string` / `reserve`、`Map.insert`、`Arena.add` —— も同じく allocator を
receiver の次に取ります(§8、§10、ADR-0132)。値は自分を作った allocator の複製を持たないので、確保にも
解放にも必要なものは呼び出し側が綴ります —— `sizeof(T)` を compile 時の値として
渡すのと同じ扱いで、原理 4「hidden allocation を持たない」の表と裏です。
owner を持つすべての型が `deinit` のこの 1 つの形を取り、導出 `deinit` は
受け取った allocator を field へそのまま渡します。

```kizu
let allocator = mem::page_allocator();
var values = array::new<i64>(allocator);
defer values.deinit(allocator);
try values.append(allocator, 7);
```

**container の構築は何も確保しません。** `std::array::new<T>(allocator)`、
`std::map::new<K, V>(allocator)`、`std::arena::new<T>(allocator)` はどれも
header そのものを作るだけです。空の `Array<T>` と `Arena<T>` は
`{data, len, cap}` の 3 word(Rust の `Vec` と同じ)、空の `Map<K, V>` は
entry 列とその index の 5 word で、allocator も element size も覚えません
(ADR-0131)。だから構築は失敗しようがなく、`!T` を返しません。storage を買うのは
最初の `append` / `insert` / `add` で、失敗を言うのもそこです。構築が受け取る
`Allocator` は compile 時の provenance で、後続の確保・解放が同じものを名指す
ことを checker が要求します。`truncate` / `clear` / `pop` / `len` / `capacity`
は確保も解放もしないので allocator を取りません。

`defer` / `errdefer` の cleanup が運ぶ引数は、**`defer` が書かれた場所で読み**、
block を出るときに走るのはその値です(§8)。

**確保に渡す `Allocator` も、解放に渡すものも、その owner を作ったものと同じで
なければなりません。** 確保だけ別の allocator から取ると、解放は自分が配って
いない byte を返すことになります —— `append` / `insert` / `add` / `reserve` と
`deinit` は 1 つの規則の表と裏です。
検査できるのは tied allocator だけです —— tie を持たない `page_allocator()`
同士は区別が付かず、区別する必要もないためです。tied allocator から作った
owner を別の tied allocator で、あるいは tie の無い allocator で解放するのは
compile error です。逆に、tied allocator から作っていない owner に tied
allocator を渡すのも error です。

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

危険な選択は禁止ではなく明示語にします。`std::mem::leak<T>(value)` は owner を
consume して解放しません —— leak-on-exit(短命 process が解放を OS に任せる)を
source に残す唯一の手段です。`std::mem::Limit` は確保上限の union で、上限を
設けない選択も `Unlimited` と綴ります。

### 14.4 std storage 型に対する検査規則

これらは API の説明ではなく、checker が持つ規則です。利用者が自分の型に
同じものを書くことはできません。

**view を返す accessor.** `String.as_bytes` は owned buffer への local
read-only view、`as_mut_bytes` は writable view(`&var []u8`)です。
どちらも戻り値を local binding に束縛する必要があります。receiver は local
binding のほか、そこを root とする field path(`owner.field.as_bytes()`)を
書けます: このとき borrow は root の該当 path に付き、その path に重なる操作と
root 全体の move が view の最終使用まで待ちます(§9)。`as_mut_bytes` は root が
mutable であることを要求します。read-only view が
生きている間は `append_bytes` / `append_byte` / `truncate` / `clear` /
`deinit` を禁止します。writable view は mutable binding の String からだけ
作れ、生きている間 String は exclusive borrow です —— すべての method 呼び出し、
`deinit`、共有 view を禁止します。書き込みは既存 bytes の上書きだけで、
length と capacity は変わりません。

**mutator の receiver.** `String` の `append_bytes` / `append_byte` /
`reserve` / `truncate` / `clear` は owned local `String` または
`&var std::string::String` から呼べます。method receiver は by-value の
parameter として書きますが、consuming transfer ではありません。

**element / value copy と container clone.** `Array.get` / `get_or_panic` と
`Map.get` は copy element / copy value 限定です。`Array.clone(allocator)` も
copy element 限定で、receiver を consume せず、指定した allocator 上に独立した
Array storage を持つ新しい owner を返します。owner element の再帰的な複製は
要素型ごとの明示的な関数で書き、汎用 clone は行いません。owner を単純に copy
すると持ち主が 2 つになるためで、owner は `at` / `at_mut` で local borrow として
読み書きします。`Array.at` / `at_mut` は borrow optional
`?&T` / `?&var T` を、`Map.at` / `at_mut` は `?&V` / `?&var V` を返します。
これを消費できるのは **capture 条件だけ**です(`if array.at(i) |elem|` /
`while m.at(key) |v|`)。binding への保存も `orelse` も拒否します。element
borrow が container の変更や解放より長生きする経路を positional に閉じる
ためで、`as_bytes` の let 限定と同じ整理です。`at_mut` は mutable な受け手
(`var` binding または `&var` 借用)を要求します。capture の scope の間
container は borrow され、shared borrow 中は `insert` / `deinit` が、
mutable borrow 中はすべての操作が capture の最終使用まで待ちます。
`Map.key_at` が返す key は、key 型が `[]u8` のときだけ map storage への view
なので capture 限定です。整数 key は値なので、その制限は付きません。

`Arena.at(handle)` は handle provenance により要素の存在を静的に扱えるため、
optional ではない `&T` を返します。直接 read するほか local binding に保存でき、
その binding の最後の使用まで arena を shared borrow します。borrow 中の `add` と
`deinit`、および borrow からの owner move は拒否します。borrow parameter を根に
持つ arena からの `&T` return はその structural provenance を caller へ引き継ぎ、
local arena からの return は borrow escape として拒否します。

**cleanup の義務.** `Array.deinit` と `Arena.deinit` は残っている initialized element を、
`Map.deinit` は保持している value を cleanup してから storage を解放します。
element / value cleanup は `T` の `deinit(self: T) -> void` を呼びます。
owner はすべてそれを持つので(§8)、場合分けはありません。

既にある owner を置き換える操作は、落ちる側の持ち主が他にいないので拒否
します。`Array.set` は owner element に対して compile error、既存 key への
`Map.insert` は owner value に対して trap です(占有しているかは実行時に
しか分からないため)。置き換えは `at_mut` で in-place に行います。

`Array.swap` は例外です。両方の initialized slot を storage 上で交換するだけで、
どちらの owner も copy・replace・cleanup しないため、owner element に使えます。
receiver は owned local または `&var Array<T>` に限り、shared borrow 越しの
呼び出しは拒否します。

`String.deinit` / `Box.deinit` / `Map.deinit` / `Arena.deinit` は caller 側の binding を
無効化する必要があるため、owned local receiver 限定です。値を保持している
場所では `owner.field.deinit(allocator)` の direct field cleanup も同じで、その field は
以後使用できません(§8)。`deinit` 後の container 使用は safe Kizu では
禁止します。

**`?Owner` field.** field が optional な owner のときも義務は同じです。
開くのは capture です。

```kizu
fn (self: Visitor) deinit(allocator: Allocator) -> void {
    if self.nick |held| {
        held.deinit(allocator);
    }
    return;
}
```

capture が束縛する payload は、値を保持している場所でだけ owner です。
そこは `self.field.deinit(allocator)` に与えているのと同じ判定で、分解できる場所と
同じです(§8)。借用越しの読みでは payload は borrow で、consume は拒否します。
payload は field storage の中にあり、借りた値では開いても渡されないためで、
`match` が owner union payload に対して持つ分け方と同じです。

開いた payload をその body で解放しないのは error です。field を開くことが
その field の義務を果たす唯一の経路なので、開いて捨てると誰も解放しません。
`deinit` が field を開かないのも error で、普通の owner field と同じく
「`deinit` は receiver の owner field をすべて consume する」に数えます。
live な field への代入も同じく error です(`Array.set` が owner element に
対して出すのと同じ拒否)。

開いて consume できるのは `if` の capture だけです。`while` の条件は毎回同じ
storage を読むので、1 周目が解放した payload を 2 周目が解放することになり、
拒否します。

**`Box<T>`.** struct / union payload に保存できます。`Box<T>` を含む
struct / union は non-copy です。`borrow` / `borrow_mut` は local borrow
source であり、戻り値は local binding に束縛します。戻り値の由来は署名から
構造的に self に tied と導出されます(ADR-0098)。borrow field は許可しません。
`take()` は local な Box を consume し、cell を解放して payload `T` を返します。
borrow が生きている間は対象 `Box<T>` の move / `take` / `deinit` を禁止します。

**element に置ける型.** `Array<T>` の element には arena、handle、nested
array、`std::map::Map<K, V>` を置けます。raw pointer と stack buffer は
置けません。この制限は struct field と union payload の中も再帰的に検査します。
owned collection が provenance または stack lifetime を保持できない型は拒否します。

**`String` は non-copy / move-only** です。

### 14.5 test 宣言

test は top-level declaration として書きます。

```kizu
import std::testing;

test "parser accepts minimal function" {
    testing::expect(true);
}
```

`kizu test <path>` は file または package root を受け取り、check 後に
top-level `test` block だけを source order で実行します。`main` は実行しません。
package 内では dependency を先にした resolved module order、同じmodule内のfile path
order、各file内のdeclaration orderで実行します。
test block は parameterless `!void` body として扱うため、helper が返す `!T` には
`try` を使えます。test block が 0 件なら失敗します。未処理 error がなければ
`test: ok` を表示します。
package test では、production fileに加えて `_test.kizu` fileを同じdirectoryのmodule
へ加えます。test fileは同じmoduleのprivate宣言を使え、test helperも同じmoduleの
他のtest fileから使えます。file-local importの規則はtest fileにも適用します。
filesystem-wide test discovery、test filter、test attribute、async test、location-aware
diagnostics、message builder helper は後続で扱います。

### 14.6 collection の実装順序

```text
std::array::Array<T>  先に検討する owned contiguous collection
[]T                   contiguous slice value
&[]T                  shared borrowed slice
&var []T              mutable borrowed slice
std::map::Map<K, V>   owned symbol table
std::set::Set<T>      後続 phase
```

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

多数の descriptor を同時に待つことは `Io` の実装差ではなく、`std::net::Poller`
という値です(ADR-0141)。

**中断できる実行系は `std::coro` として入りました**(ADR-0145)。coroutine は
並行性ではありません —— 同時に走るものは無く、`resume` が止まるところまで
走らせる間は他に何も起きません。増えるのは止まれる場所が呼び出しの途中でよい
ことで、それが `evented` な `Io` に要るものです。API の形は `docs/std/coro.md`
にあります。`async fn` / `await` は変わらず実装しません。

### 15.2 Io を取る標準 API

`std::fs`:

* `std::fs::read_file(io, allocator, path, limit)` は `!std::string::String` を返す。
  `limit: std::mem::Limit` で確保上限を明示する。超過は
  `std::fs::Error::LimitExceeded` で、`OutOfMemory`(確保失敗)とは分ける
* `std::fs::read_file_into(io, path, out: &var std::string::String)` は `!void` を
  返し、fs 側では確保しない
* `std::fs::real_path(io, allocator, path)` は `!std::string::String` を返す。
  symlink と `.` / `..` を実体へ解決するため path が存在する必要がある
  (`std::path::clean` は filesystem を読まない pure 版)
* `std::fs::write_file(io, path, bytes)` は `!void` を返す
* `std::fs::exists(io, path)` は `!bool` を返す
* `std::fs::metadata(io, path)` は `!std::fs::Metadata` を返す
* `std::fs::read_dir(io, path)` は `!std::array::Array<std::fs::DirEntry>` を返す。
  entry は name の byte 順に並ぶ。file system が返す順は host ごとに違うので、
  同じ directory がどこでも同じ listing になるよう並べ替える
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

`std::net`:

* `std::net::tcp_listen(io, address)` は `!std::net::TcpListener` を返す。
  address は `host:port`、IPv6 の host は bracket で囲む(`[::1]:8080`)。
  port 0 は host に空き port を選ばせる
* `std::net::tcp_connect(io, address)` は `!std::net::TcpStream` を返す。
  `tcp_connect_before(io, address, at)` は同じものに deadline を与える ——
  stream がまだ無いので、期限は引数として渡す
* `std::net::parse_address(address)` は `!std::net::Address` を返す。text を
  分けるだけで、名前解決はしない
* `listener.accept(io)` は `!std::net::TcpStream` を返す
* `listener.local_port()` は `!i64` を返す。port 0 で bind した listener が
  どの port になったかを言う唯一の手段
* `stream.read_into(io, allocator, out, max)` は 1 回の read が追記した byte 数を
  `!i64` で返す。**0 は相手が閉じたこと**を意味し、`max <= 0` は
  `Error::InvalidLength`
* `stream.write_all(io, bytes)` は `!void` を返す。部分書き込みは返さない
* `stream.write_some(io, bytes)` は今書けた byte 数を `!i64` で返す。**0 は
  「今は書けない」**で、error でも終端でもない。待たないので write deadline は
  掛からず、いつ再試行するかは `Poller` が言う
* deadline は**時点**であって 1 回の呼び出しの budget ではない。
  `std::net::deadline_in_millis(millis)` が今からの距離を時点にし、
  `set_read_deadline` / `set_write_deadline` / `set_accept_deadline` がそれを
  受け取る。設定した時点は以後のその向きの呼び出し**全体**を覆い、自動では
  更新されない。過ぎた後の呼び出しは待たずに `Error::TimedOut` を返す。
  `clear_*_deadline` で外す
* `std::net::poller_new(io, capacity)` は `!std::net::Poller` を返す。
  `watch_stream` / `watch_listener` が descriptor と caller の `token` を登録し、
  `wait(io, at)` が ready の個数を返し、`ready(index)` が
  `?std::net::Ready { token, readable, writable, closed }` を返す。`token` は
  caller のもので、std は読まない。`at` は deadline と同じ時点
* 1 つの descriptor が同じ `wait` で 2 回報告されることがある。kqueue は filter
  ごと、epoll は bit をまとめるので、host が言った通りを渡す
* `TcpListener` / `TcpStream` / `Poller` は非 copy の owner で、`deinit` は
  `self` を値で取る。close 後の使用は型 error であり、runtime の報告ではない
* `std::net::Address` は `host: []u8` と `port: i64` だけを持つ
* safe Kizu に file descriptor や socket pointer は出さない
* I/O failure は `!T` error として返す

`std::http`:

* `std::http::listen(io, address)` / `listen_with(io, address, limits)` は
  `!std::http::Server` を返す
* `server.accept(io, allocator)` は 1 接続を受け、request を 1 つ読み、
  `!std::http::Exchange` を返す。**handler は取らない** —— 関数値は borrow を
  運べないので handler に request を渡せず、pull の loop が残る形
* `exchange.request` / `exchange.response` は public field
* `exchange.respond(io, allocator)` / `respond_text(io, allocator, status,
  content_type, body)` は `!void` を返し、2 度目は `Error::ResponseFinished`
* `exchange.respond_head(io, allocator, framing)` は head だけを送り、body は
  caller が `exchange.write_all(io, bytes)` で書く。`std::http::Framing` は
  `Buffered`(Response が持つ body、length は実測)/ `Length(n)`(caller の申告)/
  `UntilClose`(close が終わり、length 無し)/ `Chunked`(1 write = 1 chunk、
  `finish_body` が terminator を書く)/ `Raw`(framing field を書かず、
  head は caller のもの)。送った後は answered なので `respond` は
  `Error::ResponseFinished`
* `exchange.write_all(io, allocator, bytes)` は body を書く。`Length(n)` の
  宣言を超える write は socket に届く前に `Error::ResponseOverrun` になる
* `exchange.finish_body(io, allocator)` は body を閉じる。`Chunked` は
  terminator を書き、`Length` は数が足りなければ `Error::ResponseIncomplete`。
  2 度目は `Error::ResponseFinished`。呼ばずに `deinit` した場合の残りは
  `exchange.owes()` が答える
* `server.accept(io, allocator, max)` は head を読み、body を `max` byte まで
  `exchange.body` に読む。上限が引数なのは body の大きさが endpoint の性質で
  あって server の policy ではないため
* `server.accept_head(io, allocator)` は空行で止まり、body を接続に残す。
  保持しないので上限も無い。caller は `exchange.read_into(io, allocator,
  out, max)` で読む —— `std::net::read_into` と同じ契約で、head 読みで先に
  届いていた byte から出る。待たない版が `exchange.read_ready_into(...)` で、
  `?i64` の null が「今は何も来ていない」
* `Exchange` は `TcpStream` を渡さない。`write_all` / `read_into` /
  `set_read_deadline` / `set_write_deadline` / `clear_*` が通り道で、head 読みの
  残り byte を飛ばせないのがその理由
* `std::http::Request` は method / target / version / headers を所有する。
  **body は持たない** —— body は接続から読むもので、何 byte 取るかは求める側が
  名乗る。path と query は field ではなく、`path_of` / `query_of` が target の
  中の run として返す
* `std::http::Response` は組み立ててから送る。`Content-Length` /
  `Transfer-Encoding` / `Connection` は message の実体から書き、caller が
  set したものは落とす
* `std::http::listen(io, allocator, address)` / `listen_with(..., limits)` が
  server を作り、`server.deinit(allocator)` が閉じる。allocator は多数を捌く
  ときに接続を持つ `Array` のもの
* `std::http::Limits` は request head の byte 数、header の個数、および
  head / body / write の各 phase に許す時間(ミリ秒)を caller が名指すもの。
  body の byte 数はここに無く、body を読む呼び出しの引数。時間は duration であり、各 phase が始まるときに
  deadline になる。0 はその phase に deadline を置かない
* `std::http::route(allocator, pattern, method, path, params)` は
  Go 1.22 `ServeMux` 綴りの pattern 1 つを照合して `!bool` を返す。
  routing は登録簿ではなく、呼ぶ側が書く質問
* `std::http::get` / `post` / `fetch` / `fetch_with` は
  `!std::http::ClientResponse` を返す。body の上限は引数。`https` は
  `Error::UnsupportedScheme` —— TLS を持たないので、暗号化を頼まれたものを
  平文で送らない
* `std::http::Connection` は client 側の接続と、読んで誰も取っていない byte。
  `connect` が開き、`take` が caller の stream を包む。`send` が request を
  書き、`receive` が答えの head を読んで止まり、body は `read_into` /
  `read_ready_into` が出す。`read_body` が上限つきで `response.body` に読む。
  `ClientResponse` の `body` が埋まるのはその経路だけ
* `server.first(io, allocator, max)` は完成した request を 1 つ渡し、
  `server.next(io, allocator, done, max)` は 1 つ受け取って次を渡す。受け取る形
  なので、借りた接続を返さずに次を得る道が無い。他の接続の accept / 前進 /
  期限切れの close は `next` の中で起きる。`first_head` / `next_head` は body を
  接続に残す対。loop は caller のもので、止めるのは `break`
* `server.accept_ready(io, allocator)` は待たずに接続を取り、`?Exchange` を返す。
  その exchange に request はまだ無い。`exchange.advance(io, allocator)` が届いた
  分だけ head を進め、`std::http::Progress` —— `NeedMore`(poller に戻る)/
  `Request`(head が揃った)/ `Closed`(揃う前に peer が去った) —— を返す。
  `exchange.watch_read(io, poller, token)` が poller に登録する
* `exchange.expired(now)` は今いる phase の期限が切れたかを答え、
  `exchange.refuse_expired(io, allocator)` が 408 を 1 回書く。poller は喋った
  接続しか報告しないので、黙っている相手に気づく道はこれだけ。`now` は caller が
  読む —— 接続ごとに時計を読ませない
* `exchange.next(io, allocator, max)` は同じ接続の次の request を読み、あったかを
  `!bool` で返す。`exchange.next_head(io, allocator)` は head だけを読む。false は接続が終わったこと。まだ答えていない / 自分の body を
  `finish_body` で閉じていない / `accept_head` の request body を読み切って
  いない場合は `Error::ExchangeUnfinished`
* 接続が次を運ぶのは 3 つが揃うときだけ —— `served + 1 < limits.max_requests`、
  framing が終わりを示せる(`Buffered` / `Length` / `Chunked`)、request が
  許している(HTTP/1.1 は `Connection: close` が無ければ、HTTP/1.0 は
  `Connection: keep-alive` があれば)。揃わなければ head に `Connection: close`
  を書く
* `limits.max_requests` の既定は 1。並行 API が無い(§15)ので 1 接続ずつしか
  捌けず、2 通目のために接続を保持する peer は他の全員に対して保持している
* `Transfer-Encoding: chunked` は request も response も decode する。
  `Content-Length` と併記されたら `Error::ConflictingFraming`、`chunked` 以外の
  coding は `Error::UnsupportedEncoding`、size や CRLF が読めなければ
  `Error::MalformedChunk`。chunk extension は読み飛ばし、trailer は消費して
  捨てる(上限は `max_head_bytes`)
* TLS、HTTP/2、HTTP/3 は持たない
* 並行 API が無い(§15)ので 1 接続ずつ。2 人目は listen backlog で待つ

`std::path`:

* `std::path::join(allocator, left, right)` は `!std::string::String` を返す
* `std::path::clean(allocator, path)` は `!std::string::String` を返す
* `std::path::basename(path: []u8) -> []u8`
* `std::path::dirname(path: []u8) -> []u8`
* `std::path::extension(path: []u8) -> []u8`
* path helper は pure helper であり、filesystem を読まない
* `join` と `clean` は owned buffer を構築するため、allocator を明示し、allocation
  failure を `!T` error として返す

`std::io` の具体的な API は `docs/std/io.md` が持ちます。compiler が持つ契約は、
stdio operation が `Io` capability を必ず要求し、I/O failure を error union で
返すことです。

`std::process`:

* `std::process::arg_count()` は `i64` を返す
* `std::process::arg(index)` は `![]u8` を返す
* `std::process::env(name)` は `?[]u8` を返し、未設定なら `null` を返す
  (空にしたければ `orelse ""`)
* `std::process::executable_path(allocator)` は実行中の binary の path を
  `!std::string::String` で返す。binary の隣に置いた file を、起動した
  directory に依らず見つけるための API。kernel が自プロセスについて答える
  ので argv と同じく `Io` を取らない。返るのは kernel が報告する path で、
  symlink を解決するなら `std::fs::real_path` に渡す
* `std::process::monotonic_millis()` は `i64` を返す
* `std::process::unix_millis()` は Unix epoch からの壁時計 milliseconds を `i64` で返す
* `std::process::spawn_wait8(argc, arg0, ..., arg7)` は子プロセスを起動して
  終了を待ち、`!i64` を返す。可変長引数を持たないので引数は 8 個までの固定形
* `std::process::exit_code(code)` は `i64` を返す
* `std::process::ExitStatus` は `Success` / `Failure` / `Specific(u8)` の union で、
  `main` の戻り値 `<E>!std::process::ExitStatus` としてだけ compiler が特別に扱う
  (checker が main の形を検査し、native backend が exit status へ写す)
* `std::process` helper は hidden I/O を持たない

## 16. contract / impl 方針

Kizu では、Rust trait clone ではない明示的な抽象化として、
`contract` と `impl Contract for Type;` を持ちます。

```text
contract                型が満たすべき要求
impl Contract for Type; 型が contract を満たすことの表明(任意)
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

polymorphic な呼び出しは static generic の concrete type argument を明示します。
method call は concrete type ごとに monomorphize され、通常の method lowering と
同じ経路を使います。

```kizu
fn save<W>(writer: &W, bytes: &Bytes) -> !i64 {
    return try writer.write(bytes);
}

fn main() -> !void {
    let file = File { name: "out" };
    print(try save<File>(file, Bytes { text: "hello" }));
    return;
}
```

runtime dynamic dispatch の専用型や暗黙 interface conversion はありません。

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
