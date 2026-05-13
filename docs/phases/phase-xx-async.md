Kizuのasync方針は、今のところこれです。

```text
v0:
  asyncなし

将来:
  async fn / await 構文を中心にしない
  Io / Task / TaskGroup で明示的に扱う

原則:
  asyncを隠さない
  borrowをtaskに持ち込ませない
  野良taskを許さない
  structured concurrency寄りにする
```

## Kizuのasync方針

Kizuでは、Rust風のこれを最初から入れない。

```kizu
async fn fetch(url: Url) -> Response {
    ...
}

let r = await fetch(url)
```

理由は、これを入れるとすぐに重くなるからです。

```text
- 関数に async / sync の色が付く
- Future型が言語の中心になる
- awaitをまたぐborrowが難しい
- cancellationとresource cleanupが難しい
- コンパイラ実装が重くなる
- 人間レビュー時に制御フローが追いにくくなる
```

Kizuは **memory-safe / simple / reviewable** が核なので、asyncも明示的に扱う。

## Zig寄りの `Io` capability 方式

Kizuでは、I/Oする関数は `Io` を受け取る方向にする。

```kizu
fn main(io: Io) {
    let text = fs.read_to_string(io, "config.json")
    print(text)
}
```

I/Oすることが型に見える。

```kizu
fn read_config(io: Io, path: FilePath) -> Result<String, Error> {
    return fs.read_to_string(io, path)
}
```

つまり、

```text
Ioを受け取る関数:
  外部世界に触る可能性がある

Ioを受け取らない関数:
  基本的にpure/localな処理
```

という見え方にする。

## asyncは構文ではなく実行戦略

Kizuでは、

```text
asyncは関数の性質ではなく、Ioの実行戦略
```

と考える。

たとえば、同じコードでも、

```text
BlockingIo
ThreadedIo
EventIo
TestIo
```

のような実装を差し替えられるようにする。

```kizu
fn load(io: Io, path: FilePath) -> Result<String, Error> {
    return fs.read_to_string(io, path)
}
```

この `fs.read_to_string` が blocking か event loop かは、`Io` 実装側の責任。

## 並行処理は `Task` / `TaskGroup`

将来的に並行実行したいときは、明示的に `TaskGroup` を使う。

```kizu
fn main(io: Io) -> Result<Unit, Error> {
    let group = TaskGroup()

    let a = group.spawn(io, read_file, "a.txt")
    let b = group.spawn(io, read_file, "b.txt")

    let text_a = a.await()
    let text_b = b.await()

    print(text_a)
    print(text_b)

    return Unit
}
```

ポイントは、

```text
spawnが見える
awaitが見える
groupが見える
```

ことです。

隠れたasyncを作らない。

## 野良taskは禁止

Kizuでは、こういうのは避ける。

```kizu
fn bad(io: Io) {
    io.spawn(do_work) // 戻り値を捨てる
}
```

これは危険です。

```text
taskがいつ終わるかわからない
resource cleanupが不明
エラーがどこへ行くかわからない
レビューしにくい
```

なので、Kizuでは基本的に、

```text
Taskはawaitされるかcancelされる必要がある
TaskGroupを抜ける前に全taskが完了またはcancelされる
```

という structured concurrency に寄せる。

## taskはborrowを捕まえられない

ここはかなり重要です。

Kizuでは、spawnされたtaskがlocal borrowを保持するのは禁止にする。

禁止例:

```kizu
fn main(io: Io) {
    let name = "alice"

    let task = group.spawn(fn() {
        print(name) // error: task cannot capture local borrow
    })
}
```

代わりに、taskへ渡す値は owned か copy にする。

```kizu
fn print_name(name: String) {
    print(name)
}

fn main(io: Io) {
    let name = "alice"
    let task = group.spawn(io, print_name, name)

    print(name) // error: name was moved into task
}
```

方針:

```text
taskに渡すnon-copy valueはmoveされる
taskはlocal borrowを保持できない
taskの戻り値はowned value
```

これで、Rustのasync + lifetime + `'static` まわりの複雑さをかなり避けられます。

## 仕様にするとこう

`SPEC.md` にはこの節を入れるとよさそうです。

```md
## Async policy

Kizu v0 does not support async.

Kizu may add asynchronous I/O later, but async is not a core language
feature in v0.

Kizu does not introduce `async fn` in the first async design.

Kizu prefers an explicit `Io` capability model.

Functions that perform I/O should accept an `Io` parameter.

Asynchronous execution is expressed through standard-library types such as:

- `Io`
- `Task<T>`
- `TaskGroup`

Kizu uses structured concurrency.

A spawned task must be awaited or cancelled.
Detached tasks are not allowed by default.

A spawned task may only receive owned values or copy values.
A spawned task must not capture local borrows.

Dynamic or asynchronous work must be visible in the code.
```

日本語版:

```md
## async 方針

Kizu v0 では async を実装しない。

Kizu は将来的に非同期I/Oを追加してよい。
ただし、最初のasync設計では `async fn` を導入しない。

Kizu は明示的な `Io` capability model を採用する。

I/Oを行う関数は `Io` 引数を受け取る。

非同期実行は、標準ライブラリの型で表す。

- `Io`
- `Task<T>`
- `TaskGroup`

Kizu は structured concurrency を採用する。

spawnされたtaskは、awaitまたはcancelされなければならない。
野良taskはデフォルトでは許可しない。

spawnされたtaskは、owned value または copy value だけを受け取れる。
local borrow を捕まえることはできない。

非同期実行や並行処理は、コード上で見える必要がある。
```

## まだ未決定のところ

決まっていないのは、細かい構文です。

たとえば、どれにするか。

```kizu
let task = group.spawn(io, read_file, "a.txt")
```

または、

```kizu
let task = io.spawn(group, read_file, "a.txt")
```

または、

```kizu
let task = spawn group read_file("a.txt")
```

ここはまだ後でいいです。

Kizuの現時点の決定は、構文より思想です。

```text
async fnなし
Ioを明示
Taskを明示
TaskGroupでstructured concurrency
borrow capture禁止
owned valueのみtaskへ渡す
野良task禁止
```

## まとめ

決めた方針はこれです。

```text
Kizu v0:
  asyncなし

Kizu async later:
  async fnではなくIo/Task/TaskGroup
  I/OはIo引数で明示
  task生成は明示
  dynamic/concurrent workを隠さない
  taskはborrowを捕まえない
  structured concurrency
```

Kizuらしい標語にすると、

```text
Async is explicit.
Tasks are owned.
No hidden work.
```

日本語なら、

```text
asyncは明示する。
taskは所有する。
隠れた仕事を作らない。
```
