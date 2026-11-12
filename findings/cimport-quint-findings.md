# cimport Quint モデル: 食い違いの記録

`spec/cimport/` のモデル(SPEC §12.2 の header import と、書き出した宣言の check)と、
`kizu import-c-header` + `kizu check`(Go の `internal/cimport` + `internal/types`、
`compiler/` から build した binary)を突き合わせた結果です。範囲と方法は
`spec/cimport/README.md` にあります。本体のコードは変えていません。どちらが正しいかは
仕様判断なので、ここでは決めません。

## 結論(2026-11-08、`verify/cimport-quint` の基点 71d646a)

* **受理側(安全でない写像を受理する)の所見が 3 件あります(F1〜F3)。** どれも importer が
  C の型と食い違う `extern "c" fn` を書き出し、checker はそれを受理します。Go 実装と
  `compiler/` の両方が同じ出力です(`KIZU_MBT_BIN` で確認)。
  * **F1(重大)** `const` の位置を見ない。`char *const p`(書き込まれうる `char`)が
    `ptr<const i8>` になり、read-only の memory を C の書き込み先へ渡す呼び出しが通ります。
  * **F2(重大・要判断)** C の pointer の結果を non-null の `ptr<T>` にする。NULL を
    返しうる関数の結果を、null の検査なしに non-null として持てます。
  * **F3(中・要判断)** pointee の `volatile` を黙って落とす。
* 拒否側の所見(安全だが SPEC と合わない、または書き手が驚く)が 2 件、記録だけのものが 3 件あります(F4〜F8)。
* F1〜F3 の外では、生成した 6976 本すべてで importer の出力、受理 / 拒否、診断クラスが
  モデルと一致しました。「モデルは拒否、実装は受理」の 129 本はすべて F1〜F3 で説明が付きます
  (F3 92、F2 24、F1 13。F1 の 13 本のうち 8 本は F2 も含む)。
* importer と checker に 1 か所ずつバグを入れた 29 種のうち 27 種が fixture の再生だけで落ちます。
  残る 2 種は振る舞いが変わらない変異です(末尾「変異による確認」)。
* **`quint verify`(Apalache)は Java が無いため走らせていません。** 探索は `quint test` と
  `quint run` のランダム探索だけです。さらに、quint の rust evaluator は初回に GitHub から
  取得するもので、この環境では取得できなかったため、`quint run` / `quint test` は
  `--backend typescript` で回しています(先行する spec-* の recipe は rust)。

---

## ⚠️ F1. 【重大・実装が受理してしまう】`const` の位置を見ず、どこかにあれば最内の pointee に付ける

### 最小再現

```c
void fill(char *const buf);      /* buf は const な pointer、指す char は書き換えられる */
void deep(char *const *argv);    /* const なのは中段の pointer */
```

```
$ kizu import-c-header h.h
extern "c" fn deep(argv: ptr<ptr<const i8>>) -> void     # C の型は ptr<const ptr<i8>>
extern "c" fn fill(buf: ptr<const i8>) -> void           # C の型は ptr<i8>
```

`fill` の宣言は「C は `buf` の先を読むだけ」と言っています。checker はそれを信じて、
read-only の memory を渡す呼び出しを受理します。

```kizu
extern "c" fn fill(buf: ptr<const i8>) -> void   // import-c-header の出力

fn use(a1: ptr<const i8>) -> void {
    // SAFETY: the arguments are what the C declaration asks for
    unsafe fill(a1);                              // check: ok  —  C の型どおりなら arg_type で拒否
}
```

逆向きもあります。`char **const p`(最外の pointer だけ const)は `ptr<ptr<const i8>>` になり、
C が書き込む `char` を Kizu 側は const と見ます。`const char *const *p` は C では
`ptr<const ptr<const i8>>` ですが `ptr<ptr<const i8>>` になり、C が const とする中段の pointer に
Kizu 側から書き込めます。

### SPEC の該当箇所

* §12.2「`const T*` から `ptr<const T>` への変換」。`T *const` と多段の `const` は書かれていません。
* §12「`ptr<const T>` は non-null const raw pointer」。`const` は pointee の性質です。

### どこで起きているか

* Go: `internal/cimport/importer.go` の `normalizeCBase` が型の語のどこにある `const` も
  1 つの `isConst` にまとめ、`pointerType` が `level == 0` にだけ付けます。
* `compiler/`: `compiler/src/internal/cimport/cimport.kizu` の `c_type_to_kizu` が
  `normalize_c_base` の `is_const` を最内に 1 回だけ書きます。同じ形です。
* `cmd/kizu/selfhost_test.go` の `pointers_and_qualifiers` は `char *const q` を含みますが、
  Go と `compiler/` の出力の一致を見るだけで、`ptr<const i8>` が正しいかは見ていません。

### モデルでの再現

`mutantConstCollapse`(この規則だけ importer のもの)で `declsTrue` が `initFree` の 3000 trace 以内、
`noWriteThroughConst` が `initReadonly` の数百 trace で破れます。`cimport_test.qnt` の
`constPointerTest` / `middleConstTest`、`mbt_test.go` の既知形 `constPointer` / `middleConst` が
今の出力を固定しています。

---

## ⚠️ F2. 【重大・要判断】C の pointer の結果を non-null の `ptr<T>` にする

### 最小再現

```c
char* getenv(const char* name);
```

```
$ kizu import-c-header h.h
extern "c" fn getenv(name: ptr<const i8>) -> ptr<i8>
```

```kizu
extern "c" fn getenv(name: ptr<const i8>) -> ptr<i8>   // import-c-header の出力

fn lookup(a1: ptr<const i8>) -> ptr<i8> {
    // SAFETY: the arguments are what the C declaration asks for
    return unsafe getenv(a1);                     // check: ok  —  ?ptr<i8> なら ret_type で拒否
}
```

C の pointer は NULL でありえますが、`ptr<T>` は non-null です(§12)。取り込んだ宣言を
使うと、null の検査をせずに non-null の pointer として持ち回れ、`if p |q|` で開く機会も
ありません(`ptr<T>` は開けない)。

### SPEC の該当箇所

* §12「`ptr<T>` は non-null mutable raw pointer」「`?ptr<T>` / `?ptr<const T>` は nullable raw pointer」。
* §12 の手書きの例は `extern "c" fn getenv(name: ptr<const u8>) -> ?ptr<const u8>` です。
* §12.2 は引数の例(`ptr<const i8>`)だけで、pointer の結果を何にするかは書いていません。

### 判断を要すること(決めていません)

* 結果の pointer を `?ptr<T>` にするか。引数の pointer は `ptr<T>` のままで安全側です
  (NULL を渡せない呼び出し側は C に求めるものが少ない)。
* それとも、結果が non-null であることを呼び出しの `unsafe` が引き受けるものとし、
  SPEC にそう書くか。その場合、宣言を書いたのは人ではなく importer なので、`// SAFETY:` を
  書く人が宣言の non-null を自分の約束として読むかは疑問が残ります。

`mutantRetNonNull` で `declsTrue` と `noNullLaundering` が破れます。既知形 `pointerResult` /
`pointerResultChecked` が今の出力を固定しています。

---

## ⚠️ F3. 【中・要判断】pointee の `volatile` を黙って落とす

```c
void poll(volatile int *reg);
```

```
extern "c" fn poll(reg: ptr<i32>) -> void
```

Kizu の型には volatile な pointee がありません(§12 では `volatile_read` / `volatile_write` が
操作として未証明操作の表にあるだけです)。宣言からは「この memory は volatile にしか
触ってはいけない」が消え、受け取った pointer を普通の `ptr_read` で読むことに何の手がかりも
残りません。最外の pointer 自身の `volatile`(`int *volatile p`)は値渡しなので問題ありません。

* Go: `normalizeCBase` が `volatile` を `restrict` と同じく捨てます。`compiler/` も同じ。
* SPEC §12.2 は `volatile` に触れていません。

### 判断を要すること(決めていません)

* pointee の `volatile` を拒否するか(モデルの `spec` はこちら、`Import:volatile`)、
  落とすことを SPEC に書くか。

`mutantDropVolatile` で `declsTrue` が破れます。既知形 `volatile` が今の出力を固定しています。

---

## F4. 【拒否側】`int *f(void)` は拒否、`int* f(void)` は受理

```
int *f(void);    → c import error: unsupported declaration `int *f(void)`
int* f(void);    → extern "c" fn f() -> ptr<i32>
```

同じ C の宣言が空白の位置で通ったり落ちたりします。importer の正規表現が戻り値の型と
名前の間に空白を要求するためです(Go の `functionPattern`、`compiler/` の `match_function`)。
C の header では前者が普通の書き方です。安全側ですが、§12.2 の「単純な pointer」に対応すると
読む書き手は驚きます。モデルでは `glue` として importer どおりに持っています。

## F5. 【記録】`int f()` を引数無しにする

C17 までの `int f()` は「引数を指定しない」宣言で、C23 で `(void)` と同じになりました。
importer は引数無しの `extern "c" fn f()` を書きます。K&R 形式で定義された関数を
引数無しで呼ぶことになりえます。真の対応でも引数無しとして扱い、性質の対象にしていません。

## F6. 【記録】`restrict` を落とす

`void copy(char *restrict d, const char *restrict s)` は別名を作らない約束を C に与えますが、
Kizu の宣言からは消えます。Kizu に対応する型が無く、その約束は呼び出しの `unsafe` が
引き受けるものなので、モデルでは落としてよいものとしました。

## F7. 【記録】無名引数の最後の修飾子が名前になる / 名前の重複

* `int f(int *const);` → `extern "c" fn f(const: ptr<i32>) -> i32`。型は正しく、`const` は
  Kizu の keyword ではないので check も通ります。`volatile` / `restrict` も同じです。
* `int f(int p2, int *);` → `f(p2: i32, p2: ptr<i32>)`。checker は `extern "c" fn` の
  引数名の重複を拒否しません(本体が無いので害はありません)。

## F8. 【記録】`char` は常に `i8`

`char` の符号は処理系定義です(AArch64 Linux では unsigned)。importer は target によらず
`i8` を書きます。モデルの真の対応は LP64 / `char` signed(この環境の macOS)を前提にしています。

---

## 変異による確認

importer(`internal/cimport/importer.go`)と checker(`internal/types`、`internal/project`)に
1 か所ずつバグを入れ、`go test ./internal/cimport -run CImportModelTraces`(checked-in の
fixture 837 本)を走らせて戻しました。注入用のスクリプトは関数の内部形状を文字列で
固定するもの(構造 pin)なのでコミットしていません。

`compiler/` 列は、同じ変異を `compiler/src/internal/cimport`、`compiler/src/internal/types`、
`compiler/src/internal/project` に入れて `compiler/src/cimport_mbt_test.kizu` を回したときの
fixture の食い違いの本数です(2026-11-11、同じ 837 本。括弧内は既知形の test で食い違った
形の数で、反転した判定の見逃しを含みます。括弧が無いものは既知形が通る)。29 種とも本数と
既知形の成否が Go と一致します。I19 は空の引数を C の型 `i32` として表に引かせる形
(表に無いので `Import:type`)、C5 は `i64` / `u8` を任意の scalar の引数に許す形
(モデルの `wide` は float の引数も `i64` に替える)で、Go に入れた形に合わせています。
C2 と C9 は `compiler/src/internal/types` の unit test(診断の文面を固定するもの)が先に
落ちるので、それを外して測りました。

| # | 変異 | Traces | 食い違い | 既知形 | `compiler/` |
| --- | --- | --- | --- | --- | --- |
| I1 | 表の `unsigned int` を `i32` に | 落ちる | 40 | 通る | 40 |
| I2 | 表の `char` を `u8` に | 落ちる | 39 | 落ちる | 39(7) |
| I3 | 表の `size_t` を `u64` に | 落ちる | 53 | 落ちる | 53(1) |
| I4 | 表の `float` を `f64` に | 落ちる | 51 | 通る | 51 |
| I5 | `const` を無視 | 落ちる | 446 | 落ちる | 446(9) |
| I6 | `const` を全段に付ける | 落ちる | 314 | 落ちる | 314(2) |
| I7 | `const` を最外段に付ける | 落ちる | 314 | 落ちる | 314(2) |
| I8 | 配列の検査を外す | 落ちる | 41 | 落ちる | 41(1) |
| I9 | 可変長の検査を外す | 落ちる | 29 | 落ちる | 29(1) |
| I10 | 関数 pointer の検査を外す | 落ちる | 27 | 落ちる | 27(1) |
| I11 | `typedef` の検査を外す | 落ちる | 19 | 落ちる | 19(1) |
| I12 | 無名引数の番号を 0 始まりに | 落ちる | 3 | 落ちる | 3(1) |
| I13 | 無名の pointer 引数を名前付きとみなす | 落ちる | 3 | 落ちる | 3(1) |
| I14 | `(void)` を引数とみなす | 落ちる | 321 | 落ちる | 321(5) |
| I15 | 出力を sort しない | 落ちる | 75 | 通る | 75 |
| I16 | 拒否した文を飛ばして続ける | 落ちる | 316 | 落ちる | 316(15) |
| I17 | `restrict` を落とさない | 落ちる | 401 | 通る | 401 |
| I18 | pointer の段数を 1 に潰す | 落ちる | 331 | 落ちる | 331(2) |
| I19 | 空の引数を `i32` として通す | 落ちる | 33 | 落ちる | 33(1) |
| I20 | `enum` の検査を外す | 落ちる | 18 | 落ちる | 18(1) |
| C1 | `extern "c" fn` の呼び出しに `unsafe` を要求しない | 落ちる | 88 | 落ちる | 88(11) |
| C2 | `void` 引数を許す | 落ちる | 47 | 落ちる | 47(1) |
| C3 | 引数の型の比較で `const` を無視 | 落ちる | 49 | 落ちる | 49(2) |
| C4 | 引数の型の比較で `?` を無視 | 落ちる | 2 | 落ちる | 2(2) |
| C5 | 引数の幅違い(`i64` / `u8`)を許す | 落ちる | 2 | 落ちる | 2(2) |
| C6 | 戻り値の比較で `?` を無視 | **通る** | 0 | 通る | 0 |
| C7 | 関数の重複検査を `internal/project` だけ外す | **通る** | 0 | 通る | 0 |
| C8 | `[]u8` を pointer 引数に許す | 落ちる | 6 | 落ちる | 6(2) |
| C9 | 関数の重複検査を両方(`internal/project` と `internal/types`)外す | 落ちる | 16 | 落ちる | 16(2) |

* **C6** は今の importer の下では振る舞いが変わりません。`?ptr` を返す宣言を importer が
  書かない(F2)ので、`?ptr` を `ptr` の位置へ返す呼び出し側が生成されません。F2 を直して
  fixture を作り直すと、`spec` の `Use:ret_type` の形がこの変異を落とします。
* **C7** は `internal/types` の同じ検査が同じ分類の診断(`duplicate function`)を出すので、
  観測できる振る舞いが変わりません。両方外した C9 は落ちます。
