
以下は、そのまま `SPEC.md` に貼れる形の **Kizu contract / satisfy / Dyn 仕様案** です。

位置づけとしては、**v0では未実装**、将来の `v1/v2` で導入する抽象化機能として置くのがよいです。

---

# Kizu Contract System Draft

## 目的

Kizu は Rust の `trait` をそのまま採用しない。

Kizu の抽象化は、次の3つで構成する。

```text
contract  = 型が満たすべき契約
satisfy   = 型がcontractを満たすことの明示宣言
Dyn       = 動的ディスパッチを明示する型
```

Kizu の方針:

```text
method は型に実装する。
contract は要求だけを書く。
satisfy は適合関係を明示する。
Dyn は動的ディスパッチを型に見せる。
```

Kizu は、抽象化を完全に捨てない。
ただし、抽象化がコードレビューを難しくしないようにする。

Kizu の contract system は、次のために存在する。

* 複数の型に共通する振る舞いを表す
* generic function の制約を書く
* 動的ディスパッチを明示的に扱う
* AI生成コードでも人間が追いやすい抽象化を提供する

Kizu の contract system は、次のためには使わない。

* 型レベルプログラミング
* macro的なコード生成
* 暗黙のinterface適合
* 隠れたdynamic dispatch
* Rust trait system の完全再現

---

## 基本用語

### contract

`contract` は、型が持つべきmethodの集合を宣言する。

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error>
}
```

これは実装ではない。
`Writer` である型は、指定された `write` methodを持っていなければならない、という契約である。

### satisfy

`satisfy` は、ある型があるcontractを満たすことを明示する宣言である。

```kizu
satisfy Writer for File
```

これは、

```text
File は Writer を満たす。
コンパイラは File が Writer に必要なmethodを持つか確認する。
```

という意味である。

`satisfy` は実装場所ではない。
`satisfy` block 内に関数本体を書いてはいけない。

### Dyn

`Dyn<Contract>` は、動的ディスパッチを明示する型である。

```kizu
fn save(writer: borrow Dyn<Writer>, bytes: borrow Bytes) -> Result<Unit, Error> {
    let n = writer.write(bytes)
    return Unit
}
```

`Dyn<Writer>` と書かれている場所では、runtime vtable dispatch が発生してよい。

Kizu では、dynamic dispatch を隠さない。

---

## 設計原則

### 1. method は型に実装する

Kizu では、method body は `impl Type` に書く。

```kizu
struct File {
    fd: FileDescriptor
}

impl File {
    fn write(self: borrow File, bytes: borrow Bytes) -> Result<Int, Error> {
        return os.write(self.fd, bytes)
    }
}
```

Rust のように、

```text
impl Writer for File { ... }
```

の中にmethod bodyを書く方式は採用しない。

Kizu の原則:

```text
Methods live with types.
```

日本語:

```text
methodは型のそばに置く。
```

---

### 2. contract は要求だけを書く

`contract` は、必要なmethod signatureだけを持つ。

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error>
}
```

`contract` 内には、関数本体を書けない。

禁止:

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error> {
        // error: contract cannot contain method body
        ...
    }
}
```

Kizu の原則:

```text
Contracts describe requirements, not implementations.
```

日本語:

```text
contractは要求を記述する。実装は書かない。
```

---

### 3. satisfy は適合を明示する

型がcontractを満たす場合、明示的に `satisfy` を書く。

```kizu
satisfy Writer for File
```

Goのような暗黙interface適合は採用しない。

つまり、`File` が偶然 `write` methodを持っていても、`satisfy Writer for File` がなければ `Writer` として扱われない。

理由:

* 偶然の適合を避ける
* 抽象化の意図を人間がレビューできる
* AIが勝手に曖昧な抽象化を作りにくい
* `Dyn<Writer>` 用のvtable生成点を明示できる

---

## 基本文法

### contract 宣言

```kizu
contract ContractName {
    fn method_name(self: borrow Self, arg: Type) -> ReturnType
}
```

例:

```kizu
contract Show {
    fn show(self: borrow Self) -> String
}
```

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error>
}
```

---

## self receiver

contract methodの `self` には、将来的に次を許可する。

```text
self: borrow Self       読み取りborrow
self: borrow mut Self   mutable borrow
self: Self              所有権を消費する
```

例:

```kizu
contract Close {
    fn close(self: Self) -> Result<Unit, Error>
}
```

これは `close` が値を消費することを表す。

```kizu
contract Reader {
    fn read(self: borrow mut Self, buf: borrow mut Bytes) -> Result<Int, Error>
}
```

これは `read` がreaderとbufferを変更することを表す。

v1では `borrow mut` を後回しにしてもよい。

---

## satisfy 宣言

### 省略形

method名と型が完全に一致する場合、mappingを省略できる。

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error>
}

struct File {
    fd: FileDescriptor
}

impl File {
    fn write(self: borrow File, bytes: borrow Bytes) -> Result<Int, Error> {
        return os.write(self.fd, bytes)
    }
}

satisfy Writer for File
```

これは次の省略形である。

```kizu
satisfy Writer for File {
    write = File.write
}
```

---

### 明示mapping

型側のmethod名がcontractのmethod名と違う場合、mappingを書く。

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error>
}

impl Socket {
    fn send(self: borrow Socket, bytes: borrow Bytes) -> Result<Int, Error> {
        return os.send(self.fd, bytes)
    }
}

satisfy Writer for Socket {
    write = Socket.send
}
```

意味:

```text
Writer.write は Socket.send によって満たされる。
```

---

## satisfy block の制限

`satisfy` block は、**conformance map** である。
実装blockではない。

許可:

```kizu
satisfy Writer for Socket {
    write = Socket.send
}
```

禁止:

```kizu
satisfy Writer for Socket {
    fn write(self: borrow Socket, bytes: borrow Bytes) -> Result<Int, Error> {
        return self.send(bytes)
    }
}
```

禁止理由:

```text
satisfy block に実装本体を書かせると、
Rust trait impl と同じように実装場所が分散するため。
```

Kizu の原則:

```text
A satisfy block is a conformance map, not an implementation block.
```

日本語:

```text
satisfy block は適合mapであり、実装blockではない。
```

---

## satisfy mapping のルール

`satisfy Contract for Type` は、以下を満たさなければならない。

```text
1. Contractのすべてのmethodが満たされること
2. 各methodの引数型と戻り値型が一致すること
3. self receiver が一致すること
4. mapping先は Type のmethodであること
5. mapping先に関数本体や式を書いてはいけないこと
6. 余分なmappingを書いてはいけないこと
```

例:

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error>
}

satisfy Writer for File {
    write = File.write_bytes
}
```

このとき、`File.write_bytes` は次の型を持っていなければならない。

```kizu
fn write_bytes(self: borrow File, bytes: borrow Bytes) -> Result<Int, Error>
```

---

## satisfy が失敗する例

### methodが足りない

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error>
}

struct File {
    fd: FileDescriptor
}

satisfy Writer for File
```

`File.write` が存在しない場合、エラー。

```text
error: File does not satisfy Writer

missing method:
  write(self: borrow File, bytes: borrow Bytes) -> Result<Int, Error>

help: add `File.write`, or map an existing method:

  satisfy Writer for File {
      write = File.write_bytes
  }
```

---

### 型が違う

```kizu
impl File {
    fn write(self: borrow File, text: String) -> Result<Int, Error> {
        ...
    }
}

satisfy Writer for File
```

`Writer.write` は `Bytes` を要求しているためエラー。

```text
error: File.write does not match Writer.write

expected:
  fn write(self: borrow File, bytes: borrow Bytes) -> Result<Int, Error>

found:
  fn write(self: borrow File, text: String) -> Result<Int, Error>
```

---

## generic bound

contract は generic function の制約として使える。

```kizu
fn save<W where W satisfies Writer>(
    writer: borrow W,
    bytes: borrow Bytes,
) -> Result<Unit, Error> {
    let n = writer.write(bytes)
    return Unit
}
```

この場合、`W` は `Writer` を満たす型でなければならない。

これは静的ディスパッチである。
`Dyn<Writer>` ではない。

つまり、`W = File` なら、コンパイラは `File.write` を呼ぶ。

---

## shorthand syntax

将来的に、次の短縮構文を検討してもよい。

```kizu
fn save<W: Writer>(
    writer: borrow W,
    bytes: borrow Bytes,
) -> Result<Unit, Error> {
    return writer.write(bytes)
}
```

ただし、Kizuではレビュー性を重視するため、最初は明示的な構文を推奨する。

推奨:

```kizu
fn save<W where W satisfies Writer>(...)
```

短縮形は後回し。

---

## 複数contract

1つの型は複数のcontractを満たしてよい。

```kizu
satisfy Show for User
satisfy Eq for User
satisfy Hash for User
```

generic boundでは複数指定できる。

```kizu
fn debug_key<K where K satisfies Show, K satisfies Hash>(
    key: borrow K,
) {
    print(key.show())
}
```

将来的に、構文を整理してもよい。

候補:

```kizu
fn debug_key<K where K satisfies Show + Hash>(...)
```

ただし、v1では単純な列挙でよい。

---

## Dyn

### 基本

`Dyn<Contract>` は、runtime dynamic dispatch を表す型である。

```kizu
fn log_all(logger: borrow Dyn<Logger>) {
    logger.log("started")
}
```

`Dyn<Logger>` は、内部的には次のような構造として実装されてよい。

```text
Dyn<Logger> {
    ptr: RawPtr
    vtable: LoggerVTable
}
```

ただし、ユーザーは通常vtableを直接書かない。

---

## Dyn は明示的でなければならない

Kizuは、静的dispatchから動的dispatchへ暗黙変換しない。

禁止または非推奨:

```kizu
fn run(logger: borrow Logger) {
    ...
}
```

`Logger` はcontractであり、値の型ではない。

正しい:

```kizu
fn run_static<L where L satisfies Logger>(logger: borrow L) {
    logger.log("started")
}
```

または:

```kizu
fn run_dynamic(logger: borrow Dyn<Logger>) {
    logger.log("started")
}
```

Kizuの原則:

```text
Static abstraction uses contract bounds.
Runtime dispatch uses Dyn.
```

日本語:

```text
静的抽象化にはcontract boundを使う。
動的ディスパッチにはDynを使う。
```

---

## Dyn と allocation

`Dyn<Contract>` は、hidden allocation をしてはいけない。

まずは、`borrow Dyn<Contract>` だけを考える。

```kizu
fn run(logger: borrow Dyn<Logger>) {
    logger.log("started")
}
```

これはborrowed dynamic viewであり、所有権を持たない。

将来的にowned dynamic objectが必要な場合は、明示的な型を使う。

候補:

```text
BoxDyn<Logger>
SharedDyn<Logger>
```

例:

```kizu
let logger: BoxDyn<Logger> = BoxDyn<Logger>.new(FileLogger { ... })
```

これは明示的にallocationを伴う。

Kizuでは、`Dyn<Logger>` が勝手にheap allocationを起こしてはいけない。

---

## Dyn への変換

Dynへの変換構文は、将来決める。

設計原則:

```text
1. Dyn変換は明示する
2. satisfy済みの型だけDynに変換できる
3. 変換によってhidden allocationしない
4. borrow Dynはlocal borrow ruleに従う
```

構文候補:

```kizu
let d = dyn<Logger>(logger)
```

または:

```kizu
let d = logger.as_dyn<Logger>()
```

または:

```kizu
let d: borrow Dyn<Logger> = dyn logger
```

この仕様では、具体構文は未定とする。

ただし、暗黙変換は禁止する。

---

## Dyn と borrow

`borrow Dyn<Contract>` はborrowであるため、Kizuのborrow ruleに従う。

つまり、次は禁止される。

```text
- struct field に保存する
- 関数から返す
- block外へescapeする
- borrow元をmoveする
```

例:

```kizu
struct Bad {
    logger: borrow Dyn<Logger> // error
}
```

```kizu
fn bad(logger: borrow Dyn<Logger>) -> borrow Dyn<Logger> {
    return logger // error
}
```

これはKizuの「stored borrowなし」という方針と一致する。

---

## Static contract と Dyn の比較

| 用途               | 構文                           | dispatch | allocation |
| ---------------- | ---------------------------- | -------- | ---------- |
| 具象型              | `File`                       | static   | なし         |
| 静的抽象化            | `T where T satisfies Writer` | static   | なし         |
| 動的抽象化            | `borrow Dyn<Writer>`         | dynamic  | なし         |
| 所有dynamic object | `BoxDyn<Writer>`             | dynamic  | 明示的        |

---

## 例: Show

```kizu
contract Show {
    fn show(self: borrow Self) -> String
}

struct User {
    name: String
}

impl User {
    fn show(self: borrow User) -> String {
        return self.name
    }
}

satisfy Show for User

fn debug<T where T satisfies Show>(value: borrow T) {
    print(value.show())
}

fn main() {
    let user = User { name: "alice" }
    debug(user)
}
```

---

## 例: method名が違う場合

```kizu
contract Writer {
    fn write(self: borrow Self, bytes: borrow Bytes) -> Result<Int, Error>
}

struct Socket {
    fd: FileDescriptor
}

impl Socket {
    fn send(self: borrow Socket, bytes: borrow Bytes) -> Result<Int, Error> {
        return os.send(self.fd, bytes)
    }
}

satisfy Writer for Socket {
    write = Socket.send
}
```

---

## 例: Dyn Logger

```kizu
contract Logger {
    fn log(self: borrow Self, message: borrow String) -> Result<Unit, Error>
}

struct FileLogger {
    path: FilePath
}

impl FileLogger {
    fn log(self: borrow FileLogger, message: borrow String) -> Result<Unit, Error> {
        return fs.append(self.path, message)
    }
}

satisfy Logger for FileLogger

fn run(logger: borrow Dyn<Logger>) -> Result<Unit, Error> {
    logger.log("started")
    return Unit
}
```

この例では、`run` は動的ディスパッチを使う。
それは `borrow Dyn<Logger>` と型に明示されている。

---

## coherence rule

同じ型と同じcontractの組み合わせについて、`satisfy` は1つだけ存在できる。

```kizu
satisfy Writer for File
satisfy Writer for File // error: duplicate satisfy
```

また、最初の設計では、`satisfy` は次のどちらかのmoduleでだけ書ける。

```text
1. Typeを定義したmodule
2. Contractを定義したmodule
```

両方とも外部moduleである場合は、`satisfy` を書けない。

理由:

```text
- 遠くのmoduleで勝手に適合関係が追加されるのを防ぐ
- impl衝突を避ける
- レビューしやすさを保つ
```

---

## visibility

`satisfy` は公開・非公開を持ってよい。

```kizu
pub satisfy Writer for File
```

`pub satisfy` は、そのmoduleの外でも `File satisfies Writer` として使えることを意味する。

非公開の `satisfy` は、そのmodule内だけで使える。

例:

```kizu
satisfy InternalContract for User
```

これはmodule内部の実装都合に使える。

---

## 禁止するもの

最初のcontract designでは、次を禁止する。

```text
- associated type
- generic associated type
- default method
- blanket implementation
- specialization
- implicit conformance
- hidden dynamic dispatch
- contract inheritance
- async contract method
- operator overloading
- macro-based derive
- satisfy block内のmethod body
- satisfy block内の任意式
```

理由:

```text
Kizuのcontract systemは、type-level magicのためではなく、
レビュー可能な抽象化のためにある。
```

---

## associated type を禁止する理由

Rust traitではassociated typeが強力だが、Kizuの初期設計では禁止する。

禁止例:

```kizu
contract Iterator {
    type Item // error in first design

    fn next(self: borrow mut Self) -> Option<Self.Item>
}
```

最初は、必要なら明示的な型パラメータや具象型で表す。

将来的に本当に必要になった場合のみ、慎重に再検討する。

---

## default method を禁止する理由

default method は便利だが、どの実装が動くのかを見えにくくする。

禁止例:

```kizu
contract Show {
    fn show(self: borrow Self) -> String

    fn debug(self: borrow Self) -> String {
        return self.show()
    }
}
```

最初のcontract systemでは、contract内に実装を書かない。

---

## blanket implementation を禁止する理由

Rust風のblanket implは、適合関係が遠くから発生しやすい。

禁止例:

```kizu
satisfy ToString for T where T satisfies Show
```

これは便利だが、Kizuの「明示性」と「レビューしやすさ」を損なう可能性がある。

---

## operator overloading を禁止する理由

contractを `+` や `==` などの演算子に接続すると、見た目から処理内容が分かりにくくなる。

Kizuでは、最初は演算子をprimitive型中心に限定する。

ユーザー定義型では、明示的なmethodを使う。

```kizu
a.equals(b)
```

将来的に演算子拡張を検討する場合も、かなり慎重に扱う。

---

## contract system の導入段階

Kizuでは、contract systemをv0に入れない。

推奨ロードマップ:

```text
v0:
  contractなし
  methodなしでもよい
  ownership / move / local borrow / Arena / Handle を優先

v1:
  impl Type によるmethod system

v2:
  contract 宣言
  satisfy 宣言
  satisfy mapping

v3:
  generic bound
  T where T satisfies Contract

v4:
  borrow Dyn<Contract>
  explicit dynamic dispatch

v5:
  BoxDyn<Contract> などのowned dynamic objectを検討
```

---

## Kizu contract system の一文

英語:

```text
Contracts are static requirements.
Satisfy declarations are explicit conformance maps.
Dyn makes runtime dispatch visible.
```

日本語:

```text
contractは静的な要求である。
satisfyは明示的な適合mapである。
Dynは動的ディスパッチを見えるようにする。
```

---

## Kizuらしい標語

```text
Methods live with types.
Contracts are satisfied explicitly.
Dynamic dispatch is visible.
```

日本語:

```text
methodは型のそばに置く。
契約への適合は明示する。
動的ディスパッチは隠さない。
```

または:

```text
Contracts, not magic.
Dyn, not hidden dispatch.
```

日本語:

```text
契約で表す。魔法にしない。
Dynで見せる。動的ディスパッチを隠さない。
```

---

## まとめ

Kizuの `contract / satisfy / Dyn` は、Rust trait、Go interface、Zigの手書きvtableの中間にある。

```text
Rust traitより軽い。
Go interfaceより明示的。
Zig vtableより安全で書きやすい。
Java implementsより静的dispatchに寄せられる。
```

Kizuの目的は、抽象化をなくすことではない。

目的は、

```text
抽象化を明示し、人間がレビューできる形に保つこと。
```

そのために、Kizuでは次を採用する。

```text
contract:
  契約を書く

satisfy:
  契約への適合を明示する

mapping:
  contract method と型methodの対応を明示する

Dyn:
  runtime dispatch を型に見せる
```
