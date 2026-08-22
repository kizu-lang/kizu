# ADR-0089: `unsafe` は式のマーカーにし、不変条件は型に印を付ける

Status: 採用

Supersedes: `@unsafe(capability, ...) { ... }` を block として持つ設計

## 背景

先行する判断は `unsafe { ... }` を退け、`@unsafe(capability, ...) { ... }` を採用していた。
理由はこうだった。

> 従来の `unsafe { ... }` は境界を明示できますが、block 内でどの種類の unsafe
> operation を許しているかをコード上で区別できません。

この判断は、**block が「義務の範囲」を示す**という前提に立っている。その前提が
成り立たないことが分かった。

### block は義務の範囲を示さない

範囲を決めるのは block ではなく privacy である。Rust も同じ結論に達している。

> to evaluate the soundness of `unsafe` code, it is not enough to check the
> contents of `unsafe` blocks — one must check all places (including safe
> contexts) in which safety invariants might be violated.
> — Rustonomicon / RFC 3458

Kizu で再現できる。次は `check: ok` になる。

```kizu
struct Buf { data: ptr<u8>, len: usize }

fn set_len(b: &var Buf, n: usize) -> void {   // unsafe が 1 文字も無い
    b.len = n;
}

fn read(b: &Buf) -> u8 {
    @unsafe(ptr_read) { return ptr_read(b.data); }   // 1 式。これ以上縮まない
}
```

`read` の block をどれだけ縮めても、`read` の正しさは `set_len` を読まないと
判断できない。そして `set_len` は `unsafe` の grep に出ない。

前提が消えると、block に残る正当化は繰り返しの削減、つまり ergonomics だけになる。
そして今の Kizu にはそれが効くコードが 0 行ある。正例の `@unsafe` は 4 file
8 箇所で、すべて操作 1 個である。

### capability list は識別子空間の中にいる

capability 名は予約語ではない。次は `42` を 2 回印字して終わる。

```kizu
fn main() {
    let ptr_deref = 42;
    print(ptr_deref);
    @unsafe(ptr_deref) { print(ptr_deref); }   // この ptr_deref は変数ではない
}
```

Kizu がコンパイラ予約名を書く場所は 5 つあり、4 つは string literal である
(`extern "c"`、`@repr("c")`、`@link_name("puts")`、`@link_lib("c")`)。
`@unsafe(ptr_deref)` だけが裸の識別子を使っている。

### `@` は `unsafe` に対して仕事をしていない

`unsafe` は既に予約語である(`internal/token/token.go:92`)。`@unsafe` は
`token.At` + `token.Unsafe` の 2 トークンで、`@` を外しても構文は一意に決まる。

一方 `requires_unsafe` / `repr` / `link_name` / `link_lib` は予約語ではない。
これらの `@` は「予約語にせずに済ませる」という仕事をしている。`@unsafe` の
`@` だけが重複している。

### 8 個の capability は 2 種類の別物である

| 種類 | capability | 式の綴りが名乗るか |
| --- | --- | --- |
| 操作 | `ptr_read` `ptr_write` `ptr_deref` `ptr_cast` `ptr_int_cast` `volatile` | ○ |
| 契約 | `extern_call` `unsafe_call` | ✗ 呼び先の宣言が持つ |

操作は自分を名乗れるが、契約は呼び先にあるから名乗れない。先行する判断は
この 2 種類を 1 つのリストに混ぜていた。Rust が RFC 2585 で分離したのと同じ兼務
である。

> this use of `unsafe` both declares the existence of a contract to call the
> current function, and declares that the contracts of the unsafe operations
> inside this function are being upheld

Rust は edition 2024 で `unsafe_op_in_unsafe_fn` を deny 既定にして分離した。

## 決定

### 1. `unsafe` は式の前置マーカーにする

`@unsafe(capability, ...) { ... }` を廃止し、危険な式の直前に `unsafe` を置く。

```kizu
fn read_tag(node: ptr<const Node>) -> i64      { return unsafe node.*.tag; }
fn write_tag(node: ptr<Node>, tag: i64) -> void { unsafe node.*.tag = tag; }
fn read_byte(p: ptr<const u8>) -> u8            { return unsafe c::raw_byte(p); }
fn cast_to_mut(p: ptr<const u8>) -> ptr<u8>     { return unsafe cast<ptr<u8>>(p); }
```

Kizu は既に `try` と `comptime` を式の前置 keyword として持つ。`unsafe` は
その棚に入る。新しい構文カテゴリを足さない。

capability 名は source から消える。操作 6 個は式の綴りが名乗り、契約 2 個は
宣言(`unsafe fn` / `extern "c" fn`)と module path が名乗る。

```kizu
import app::c;
return unsafe c::raw_byte(p);   // `unsafe` が危険を、`c::` が C 境界を名指す
```

Kizu の import は module を束縛し symbol を束縛しないので(SPEC §6.6)、
module 外の呼び出しには必ず path が付く。

capability 8 個は診断メッセージの中に残る。`internal/unsafecap` は診断の
help 文の供給元として維持する。

### 2. `unsafe fn` を採用し、`@requires_unsafe()` を廃止する

呼び出し側に memory safety obligation を渡す関数は `unsafe fn` と宣言する。

```kizu
unsafe fn raw_write(p: ptr<u8>, v: u8) -> void { unsafe ptr_write(p, v); }

fn caller(p: ptr<u8>) -> void { unsafe raw_write(p, 1); }
```

`unsafe fn` は一度採り、一度退けた。退けた根拠は「本体を丸ごと unsafe block に
してしまう」であり、それは Rust の実装の性質であって語の意味ではない。Rust 自身が
RFC 2585 でその実装を捨てた。

`unsafe fn` の本体は通常の関数本体である。本体の危険な式には、それぞれ
`unsafe` が要る。

`requires_unsafe` を予約語にする必要がなくなるので、keyword は 30 語のまま。
宣言位置の `unsafe` は parser が既に捕まえている(`internal/parser/parser.go:70`,
`136`)ので、error を実装に差し替える。

### 3. raw pointer field を持つ struct は `unsafe struct` を必須にする

```kizu
/// data は少なくとも cap 要素分の有効なメモリを指す。
/// len <= cap が常に成り立つ。
unsafe struct Array<T> {
    data: ptr<T>,
    len: usize,
    cap: usize,
}
```

`ptr<...>` / `?ptr<...>` を field に持つ struct を `unsafe struct` と宣言しないのは
compile error にする。判定は型注釈の構文検査で足りる。

抜け道は今の言語に存在しない。

| 抜け道 | 状態 |
| --- | --- |
| type alias で別名にする | 言語に無い(SPEC §0.2 / §7)。ただし導入するかどうかは未検討で、永続的な非目標ではない |
| generic struct の型引数に隠す | 言語に無い(SPEC §7「full generics を実装しません」) |
| Array 要素に入れる | 既に拒否(`examples/negative/std_array_struct_raw_pointer_element.kizu`) |
| union variant の payload や std container の要素に裸の pointer を入れる | 印は付かない。ただしそこには pointer 1 つしか無く、ずれる相手の field が無い。決定 3 が閉じたいのは `data` と `len` が独立に動ける形で、多 field の payload は struct にするほか無く、その struct は規則に掛かる |

C layout struct(SPEC §12.2 が `extern struct` / `@repr("c")` として将来定める形)
は対象外にする。C ABI struct は field を `pub` にできないと構築できず、
`import-c-header` は C の名前をそのまま使うため改名も課せない。根拠は SPEC §0.1
が既に持っている ——「raw pointer operation、C ABI call、unchecked operation は
safe Kizu の保証外です」。例外ではなく、元から線の外である。

### 4. `unsafe struct` は `pub` field を持てず、書き込みに `unsafe` が要る

- `unsafe struct` の field に `pub` を付けるのは compile error
- field への**書き込み**には `unsafe` が要る
- **構築**にも `unsafe` が要る
- field の**読み**には要らない

構築を書き込みと同じ扱いにするのは、そうしないと規則に穴が開くからである。
読みが自由なので、safe code が他の値から raw pointer を取り出して、不変条件を
満たさない値を組み立てられてしまう。

```kizu
fn forge(b: &Buf) -> Buf {
    return Buf { data: b.data, len: 4096 };   // 印が無ければ safe code で通る
}
```

これは RFC 3458 が copy を `unsafe` にした理由と同じ場面である。Kizu は
読みではなく構築の側に印を置くことで、同じ穴を閉じつつ読みを解放する。

読みを解放できるのは Kizu 固有の性質による。**Kizu では raw pointer を手に
入れても、使うのに `unsafe` が要る。** 次は前者が通り後者が落ちる。

```kizu
fn steal(b: &Buf) -> ptr<u8> { return b.data; }        // check: ok
fn use_it(b: &Buf) -> u8 { return ptr_read(b.data); }  // error: requires unsafe
```

Rust RFC 3458 が読みも `unsafe` にしたのは「a field may carry an invariant that
could be violated as a consequence of a copy」による。Kizu では所有型を持ち出す
のは move 検査が押さえ、raw pointer は使うまで不活性なので、この理由が当たらない。

読みを解放する効果は大きい。読みも `unsafe` にすると、pointer 容器の実装では
`unsafe` がほぼ全行に現れ、grep が絞り込みとして機能しなくなる。

```kizu
fn (self: &Array<T>) len<T>() -> usize     { return self.len; }
fn (self: &Array<T>) get<T>(i: usize) -> T { if i >= self.len { ... }
                                             return unsafe ptr_read(...); }
fn (self: &var Array<T>) append<T>(v: T)   { unsafe self.len = self.len + 1; }
```

`pub` 禁止に加え、`unsafe struct` の構築とfield書き込みを宣言fileに限定すると、
監査範囲が言語規則としてfile 1枚に固定される。Kizuの通常のprivate宣言は同じ
directory moduleの全fileから使えるが、別fileは `unsafe` を付けてもその型の不変条件を
作り変えられない。読み出したraw pointerを使う操作には従来どおり `unsafe` が要る。

### 5. 義務を作る場所と果たす場所の両方で、理由の記述を必須にする

memory safety obligation の中身はコードに書けない。書ける場所は comment だけで
ある。checker が検査できるのは「書いてあるか」だけで、中身の正しさは検査できない。
それでも、書いていないことは検査できる。

義務は 2 箇所で発生する。**作る場所**(`unsafe fn` / `unsafe struct`)と、
**果たす場所**(`unsafe` を含む文)である。両方で必須にする。

#### 5.1 義務を作る場所 — `///` を必須にする

```kizu
/// data は少なくとも cap 要素分の有効なメモリを指す。
/// len <= cap が常に成り立つ。
unsafe struct Array<T> {
    data: ptr<T>,
    len: usize,
    cap: usize,
}

/// p は少なくとも 1 バイトの書き込み可能なメモリを指していなければならない。
unsafe fn raw_write(p: ptr<u8>, v: u8) -> void { ... }
```

`unsafe struct` は不変条件を、`unsafe fn` は呼び出し側の前提条件を書く。
`///` が無い、または本文が空なら compile error にする。

Kizu は `///` を持ち(SPEC §6.3)、AST が `Doc` / `MemberDocs` を保持しているので、
検査は宣言を見るだけで済む。Rust にも同じ位置に `clippy::missing_safety_doc` が
ある。

#### 5.2 義務を果たす場所 — `// SAFETY:` を必須にする

`unsafe` を含む文の直前に `// SAFETY:` を必須にする。

```kizu
fn read_tag(node: ptr<const Node>) -> i64 {
    // SAFETY: node の有効性は呼び出し側が保証する(unsafe fn の契約)
    return unsafe node.*.tag;
}

fn (self: &var Array<T>) append<T>(v: T) -> !void {
    ...
    // SAFETY: 直前に cap を確認済みなので len <= cap は保たれる
    unsafe self.len = self.len + 1;
}
```

規則は次のとおり。

- 単位は**式ではなく文**である。1 つの文に `unsafe` が複数あっても
  `// SAFETY:` は 1 つでよい(`unsafe ptr_write(p, unsafe ptr_read(q))` など)
- 接頭辞は ASCII 固定の `// SAFETY:` とする。後続の本文は自由記述で、日本語でよい
- 本文が空なら compile error にする
- 検査するのは存在だけで、内容の妥当性は検査しない

Rust std は同じ規約を持ち、clippy に `undocumented_unsafe_blocks` がある。
Rust では規約と lint だが、Kizu は言語規則にする。理由は 2 つある。
`unsafe` を書く側が unsafe を理解しているとは限らず、規約は理解している人に
しか効かない。そして Kizu には既存コードが 8 箇所しかなく、今なら必須にできる。

**この決定は lexer の変更を要求する。** 現在 `internal/lexer/lexer.go:74` は
`//` 行コメントを読み捨て、保持するのは `///` だけである(`skipLineComment` と
`clearDocComments`)。`// SAFETY:` を token に載せる必要がある。`token.Token` は
既に `DocComments []string` を持つので、同じ機構を広げる。

## 強制できる範囲

この決定の要点は、**実装者が unsafe を理解していなくても規則が発火する**ことに
ある。`data: ptr<T>` と書いた時点で `unsafe struct` が要求され、そこから `pub`
禁止・書き込みの `unsafe`・`///` が連鎖する。

| 規則 | 強制 |
| --- | --- |
| 操作に `unsafe`(8 経路) | 完全 |
| `unsafe fn` の**呼び出し**に `unsafe` | 完全 |
| `ptr<>` field を持つ struct に `unsafe struct` | 完全 |
| `unsafe struct` の `pub` field 禁止 | 完全 |
| `unsafe struct` の構築と field 書き込みに `unsafe` | 完全 |
| `unsafe fn` / `unsafe struct` の `///` の存在 | 完全(存在のみ) |
| `unsafe` を含む文の `// SAFETY:` の存在 | 完全(存在のみ) |
| 関数を `unsafe fn` と**宣言**させる | **不可** |
| address を `usize` に符号化した struct の検出 | **不可** |

後者 2 つは意図の宣言であり undecidable である。どちらも今日すでに穴として存在
する。`examples/requires_unsafe.kizu` の `raw_increment` は unsafe 操作を 1 つも
持たないまま `@requires_unsafe()` を宣言して通っている。この決定で悪化はしない。

`usize` 符号化の経路は、使用側が必ず marked になる(`ptr_from_int` は `unsafe` を
要求する)。素直に `ptr<T>` と書くより手間が増え、書いた `unsafe` は grep に出る。
知らずに落ちる穴ではない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `@` を残したまま capability を literal 化する | `unsafe` は既に予約語で、`@` は重複したまま |
| `unsafe(cap) { }`(`@` だけ落とす) | 括弧付き引数リストを取る keyword が Kizu に 1 つも無い。capability は 18/19 が 1 個で、複数は nesting で書ける |
| block の上に attribute を置く | Kizu は文に attribute を付けられない。MoonBit も文法で宣言限定と定めている |
| capability を値として渡す(Austral 方式) | 偽造不能性が前提。Kizu の `std::io::blocking()` / `std::mem::page_allocator()` は引数なしで呼べ、`main` に capability を受け取る口も無い |
| capability を関数境界で伝播させる | 伝播は宣言に書く言語の性質だが、Kizu の正例は 19 箇所すべて深さ 1 で終端しており、`@requires_unsafe()` の正例は 1 つだけ。適用対象が 0 |
| 効果を推論する(Nim 方式) | 源泉が source から消える。明示性と衝突する |
| unsafe module の印(Austral `Unsafe_Module` 方式) | 不要。`grep 'ptr<'` が監査対象の型を示し、`unsafe struct` の不変条件を作り変える操作は宣言 file に閉じるため、module 全体へ印を広げる必要がない |
| 契約側の関数名に印を強制する | `import-c-header` は C の名前をそのまま Kizu 名にする。改名を課すと man page にも header にも無い名前が生まれる |
| field 単位の印(Rust RFC 3458 方式) | 読み・copy・参照すべてに `unsafe` を要求するため、pointer 容器で `unsafe` が全行に出て grep が絞り込みにならない。決定 3/4 は型に印を付けて読みを解放する |
| 実行時の不変条件検査(Ada `Type_Invariant` / D `invariant`) | public method の出入りに自動で検査が挿入されるのは隠れた制御フローである。静的証明版(SPARK)は SPEC §2 の非目標 |

## 影響

- `@unsafe(...)` 文は消える。`token.At` の文位置受け口(`internal/parser/parser.go:708`)
  ごと削除する。`@` が付く語は `@repr` / `@link_name` / `@link_lib` だけになり、
  **`@` = 宣言に付く attribute、引数は string literal** という 1 本の規則に揃う
- `unsafe` は 3 箇所に現れ、意味は 1 つ(コンパイラが証明しない。人間が持つ)

  | 位置 | 綴り |
  | --- | --- |
  | 式 | `unsafe ptr_read(p)` |
  | 関数宣言 | `unsafe fn raw_write(...)` |
  | struct 宣言 | `unsafe struct Buf { ... }` |

- keyword は 30 語のまま増えない
- 書き換え対象は正例 8 箇所(4 file)と `examples/negative/` の unsafe 系
- `examples/negative/unsafe_moved_value.kizu` は主張ごと書き換えが要る。
  現在の本体は `@unsafe(ptr_read) { print(name.value); }` で、unsafe 操作を
  1 つも使っていない。「`unsafe` の中でも moved value は error」を保つには、
  本物の unsafe 操作を使う形にする
- `lib/kizu/std/src` に `@unsafe` も `ptr<` も 0 件なので、std の書き換えは無い
- `internal/fmt` と `internal/lsp` は新 surface に追従する。LSP の capability
  completion は不要になる
- `internal/lexer` は `// SAFETY:` を読み捨てずに token へ載せる。現在は
  `///` 以外の行コメントをすべて捨てている
- `internal/fmt` は `// SAFETY:` を対応する文に固定して出力する

## 移行順序

1. **未使用 capability を error にする**(単独 PR)。今は
   `@unsafe(ptr_write, volatile, extern_call, ptr_int_cast) { print(1); }` が
   exit 0 で通る。`internal/types` の `unsafeCaps` は平坦な map を入れ子ごとに
   コピーしていて出所が消えるので、親子を鎖にして最も内側の与えた枠に使用印を
   立てる。副産物として入れ子の重複宣言も検出できる。
   これを先に入れないと、式形への機械的な書き換えが正しいか判定できない
2. `unsafe` 式マーカーと `unsafe fn` を実装し、`@unsafe` / `@requires_unsafe` を
   削除する
3. `unsafe struct` と決定 3/4 を実装する
4. 決定 5 を実装する。`///` の存在検査が先、`// SAFETY:` は lexer 変更を伴う
   ので後段にする
5. SPEC §12 を書き換える。capability 表は診断の説明として残す

`comptime if` は選ばれた branch だけを検査するので(SPEC §13)、選ばれなかった
branch でしか使わない capability は未使用と判定される。挙動としては正しいが、
1 の規則として明記する。

## 再評価条件

決定 4 の「読みは `unsafe` を要さない」は、**raw pointer が使うまで不活性である**
ことに依存する。safe な pointer 算術や safe な添字を導入する変更を入れる場合は、
読み側の解放を同じ変更で見直す。

決定 3 の閾値(`ptr<>` を持つ struct)は、raw pointer を持たない struct が
不変条件を担う場合を捕まえない。Kizu 製の pointer 容器が書かれ、その形が実際に
現れた時点で再評価する。

type alias を導入する場合も決定 3 を見直す。checker は型を解決してから判定すれば
よいので強制は保てるが、`grep 'ptr<'` が正確な監査集合を返すという性質は失われる。

```kizu
type Raw = ptr<u8>;
struct Buf { data: Raw, len: usize }    // ptr< が綴りに現れない
```

この grep 性質は、決定 3 で unsafe module の印を不要と結論した根拠そのものである。
type alias が入るなら、印の要否をもう一度検討する。
