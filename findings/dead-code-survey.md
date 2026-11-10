# デッドコードの棚卸し(#1962 マージ後の main)

基点: `3856538`(#1962 のマージ)。コードは変更していない。

## 結論

| 対象 | A: 安全に消せる | B: 要注意 | C: 消せない |
| --- | --- | --- | --- |
| Go(`internal/` `cmd/` `scripts/`) | 4 件 / 約 110 行 | 12 件 / 約 70 行 | 8 群(marker method ほか) |
| selfhost(`compiler/src/`) | 20 件 / 257 行 | 37 件 / 約 580 行 | — |
| std(`lib/kizu/std`) | 4 件 / 22 行 | 1 件 / 3 行 | 16 件(公開 API) |
| **合計** | **28 件 / 約 390 行** | **50 件 / 約 650 行** | |

- 最大の候補は **stdmethod の旧 method index**。Go 側(`internal/stdmethod/methods.go`
  の約 90 行)はどこからも呼ばれず、Kizu 側(`compiler/src/internal/stdmethod/stdmethod.kizu`
  の約 205 行)は自分の test からしか呼ばれない。両側を 1 PR で消すのが筋。
- **#1962 由来の残骸は見つからなかった。** 単一モジュール経路は `--opt` と
  `kizu ir` が今も使っている(§4)。
- 行数は定義本体の行数(Kizu は直前の doc comment を含まない。Go は含む)。対応する
  test block の行数は含まない。

## 調べ方

### Go

| ツール | 範囲 | 結果 |
| --- | --- | --- |
| `deadcode -test ./...`(x/tools v0.31.0, RTA) | 全 main package + test binary | 64 件。52 件は `ast` の marker method、8 件は `typ` の marker method、残り 4 件 |
| `deadcode ./...`(test なし) | main package のみ | 78 件。差分 14 件が test からしか届かない関数 |
| `GOOS=linux deadcode -test ./...` | 同上 | darwin と同一(64 件) |
| `staticcheck -checks U1000 [-tests=false]` | 非公開識別子 | test なしで 1 件(`isBufferType`)、test 込みで 0 件 |
| 自作の go/types 走査 | 公開を含む package-level の func / var / const / type と struct field の `Uses` | test 込みで 41 件。大半は C(埋め込み field、JSON field、iota 定数) |
| `go vet ./...` | — | 指摘なし |

- `//go:build` 行を持つファイルは `internal/` `cmd/` に 0 件。OS 分岐は実行時の
  `Target` 判定で、build tag は無い。cgo も無い。
- 各候補は最後に `git grep -w <name>` で全ファイル(Kizu、docs、scripts、workflow を含む)を確認した。

### selfhost(Kizu)

専用ツールが無いので、行頭の `fn` / `struct` / `union` / `enum` / `contract` 定義を
item として切り出し、comment と文字列リテラルを除いた本体の識別子で到達性を取った。

- 根: `compiler/src/main.kizu` の `main`、`impl` 宣言、`extern` 宣言。test 根は `*_test.kizu`
  の全 item(selfhost の test block はすべて `*_test.kizu` にある)。
- 自由関数と型は module 修飾で照合した(他 module からは `<module>::name`、同 module
  内は素の名前)。method は呼び出しが `.name(` なので名前だけで照合した。同名 method が
  1 つでも生きていれば生扱いになる保守側の近似で、**見逃しはあり得るが誤検出はしない**。
  実際 stdmethod の型群は `find` / `substitute` / `copy` という同名 method に隠れて
  機械的には出ず、手で追った。
- 機械抽出した候補は全件 `grep -rnw` で参照 0(または test のみ)を確認した。

### std

std の item を同じ方法で切り出し、std 自身・`compiler/`・`examples/`・`tests/` の Kizu
と、Go ソース(文字列中の名前を含む)、`docs/`、`SPEC.md` を参照元として数えた。

- `pub fn` は非 `internal` module なら公開 API。method は `pub` が無くても型と一緒に公開
  される(`docs/std/*.md` が method を API として載せている)ので、公開型の method も
  公開 API として扱った。

## 1. Go

### A: 安全に消せる

| 場所 | 定義 | 行 | 根拠 |
| --- | --- | --- | --- |
| `internal/stdmethod/methods.go:8-97` | `Method` `ParamType` `Method.Substitute` `MethodIndex` `IndexMethods` `methodFromDecl` | 90 | deadcode が 3 関数を unreachable と報告。型 3 つは `git grep` で package 外の参照 0、package 内でもこの 3 関数からしか使われない。test も無い。同ファイルの `MethodName` / `SplitMethodName` / `CallName` は生きている |
| `internal/stdmeta/stdmeta.go:173` | `Names` | 8 | deadcode unreachable、test 込みでも参照 0。doc は「test が列挙するため」だがその test が無い |
| `internal/types/type.go:272` | `(*typeTable).isBufferType` | 9 | staticcheck U1000(test なし)。`git grep -w isBufferType` は定義行のみ(似た名前の `ownership.isBufferTypeName` は別物)。Kizu の `is_buffer_type` は本体から使われており対にならない |
| `internal/stdprim/registry.go:236` | `TypedCoreBuiltins` | 4 | go/types 走査で Uses 0、`git grep` も定義行のみ。対の Kizu `stdprim::typed_core_builtin` は test のみ(§2 B) |

### B: 要注意(test からしか呼ばれない)

いずれも `deadcode ./...` で unreachable、`deadcode -test` で reachable になる。
多くは「既定 target で呼ぶ」薄い wrapper で、消すなら test を `*ForTarget` などに書き換える。

| 場所 | 定義 | 行 | 呼び出し元 | 備考 |
| --- | --- | --- | --- | --- |
| `internal/ir/reachable.go:8` | `KeepReachableFunctions` | 6 | `reachable_test.go` のみ | 本体は `KeepTargetReachableFunctions` と共通の `keepReachableFunctions`。Kizu 側の `keep_reachable_functions` は参照 0(§2 A)。消すなら test 3 本を `KeepTargetReachableFunctions` に移す |
| `internal/ir/lower.go:23` | `Lower` | 4 | ir / llvm の test | `LowerForTarget(…, MustHost())` の wrapper |
| `internal/ownership/checker.go:282` | `New` | 4 | test | `NewForTarget(MustHost())` の wrapper |
| `internal/types/checker.go:116` | `New` | 4 | test | 同上 |
| `internal/stdtarget/stdtarget.go:82` | `MustHost` | 10 | test と上の 3 wrapper | 上の wrapper を消すと test 専用になる |
| `internal/wasm/emit.go:20` | `Emit` | 8 | test | 本体経路は `LowerTarget` |
| `internal/wasm/emit.go:29` | `Lower` | 4 | `Emit` と test | 同上 |
| `internal/lsp/completion.go:77` | `Complete` | 4 | test | server は追跡中 document 用の `(*Server).completions` を使う |
| `internal/lsp/inlay_hint.go:11` | `InlayHints` | 4 | test | 同上(`(*Server).inlayHints`) |
| `internal/parser/parser.go:41` | `(*Parser).Errors` | 8 | parser / cmd の test | test の assert 用。残す価値あり |
| `internal/stdprim/registry.go:388` | `BuiltinNames` | 9 | `cmd/kizu/builtin_namespace_test.go` | 名前空間を閉じる test の列挙元。残す価値あり。Kizu 側 `builtin_names` も test のみ |
| `internal/unsafecap/unsafecap.go:63` | `All` | 6 | `mbt_test.go` | MBT の列挙元。残す価値あり。Kizu 側 `unsafecap::all` も test のみ |

### C: 消せない(ツールは未使用と言うが到達経路がある)

| 場所 | 定義 | 理由 |
| --- | --- | --- |
| `internal/ast/ast.go` | `declNode` / `statementNode` / `expressionNode`(52 個) | sealed interface の marker method。呼ばれないことが役割で、消すと `Decl` / `Statement` / `Expression` を何でも満たせてしまう |
| `internal/typ/typ.go:112-133` | `typeNode`(8 個) | 同上(`typ.Type`) |
| `internal/ast/ast.go` | 埋め込み field `DeclSpan` / `StmtSpan` | method の昇格に使う。field 名としての参照が 0 なだけ |
| `internal/project/std.go:41,235` `internal/stdlib/stdlib.go:36` | 埋め込み `sync.Once` | 同上(`Do` の昇格) |
| `internal/lsp/protocol.go` | `JSONRPC` `Range` `Detail` `LanguageID` `Version` `ActiveSignature` | LSP wire 形式の JSON field。読まなくても decode / encode に要る |
| `scripts/cimport-mbt/main.go:260` | `use.Ret` | JSON field |
| `internal/lsp/semantic_tokens.go:12` `internal/ownership/checker.go:1302` | `semanticParameter` `exitPlain` | iota 列の一員。消すと後続の値がずれる(`semanticParameter` は LSP の token type 番号) |
| `internal/stdlib/stdlibtest/stdlibtest.go:17` | `RepoLibDir` | test 補助 package の唯一の役割。test のみから呼ばれるのが正しい |

## 2. selfhost(`compiler/src/`)

### A: 安全に消せる(repo 全体で参照 0)

対の Go 実装は「無い」か「Go 側では生きていて Kizu は別の関数で同じことをしている」の
どちらかで、どれも Kizu だけを消して両実装の挙動は変わらない。

| 場所 | 定義 | 行 | Go 側 |
| --- | --- | --- | --- |
| `internal/ir/ir.kizu:793` | `takes_address_of` | 13 | llvm 側が `llvm/call.kizu:316` に private の同名関数を持ち、そちらだけが使われている |
| `internal/ir/lower.kizu:1724` | `(Lowerer).callee_params` | 10 | 無し |
| `internal/ir/reachable.kizu:15` | `keep_reachable_functions` | 8 | `KeepReachableFunctions`(test のみ、§1 B)。消すなら一緒に |
| `internal/ir/slots.kizu:223` | `remove_slot` | 16 | 無し |
| `internal/llvm/array.kizu:220` | `array_element_addr` | 11 | Go の `arrayElementAddr` は生きている。Kizu は別経路 |
| `internal/llvm/llvm.kizu:2562-2610` | `ContainerKind` `is_container_type` `write_container_new` | 48 | 無し。3 つで閉じた塊(`write_container_new` が唯一の利用者) |
| `internal/llvm/types.kizu:271` | `llvm_binary_op` | 9 | Go の `llvmBinaryOp` は生きている |
| `internal/ownership/comptime.kizu:127` | `read_comptime_expr` | 10 | Go は生きている |
| `internal/ownership/container.kizu:478` | `not_an_arena_error` | 17 | 無し |
| `internal/ownership/helpers.kizu:751` | `coerce_ownership_borrow_argument` | 26 | 無し |
| `internal/ownership/meta.kizu:325` | `meta_generic_base` | 10 | Go は生きている |
| `internal/ownership/metadata.kizu:437` | `field_borrow_count` | 12 | 無し |
| `internal/ownership/type_apply.kizu:1870` | `is_consume_primitive` | 3 | Go は生きている |
| `internal/typ/typ.kizu:701,711` | `nullable_pointer_elem` `openable_elem` | 18 | 呼び出しはすべて `ir::NameTable` / `types::TypeTable` / `ownership::NameTable` の同名 method へ行っている |
| `internal/types/metadata.kizu:86,94` | `is_user_declared_type` `is_public_type` | 39 | Go は `rejectPrivateTypeExcept` で使う。Kizu は同じ検査(`exposes private type`)を `types/public_api.kizu:320` で別に持つ |
| `internal/wasm/layout.kizu:286` | `is_tag_type` | 7 | Go は生きている |

消し残しの参照があれば、本体側は `kizu check compiler`(commit hook)、test 側は
`go test ./internal/selfhost` で落ちる。

### B: 要注意

**B-1. test からしか呼ばれない実装(消す候補)**

| 場所 | 定義 | 行 | 備考 |
| --- | --- | --- | --- |
| `internal/stdmethod/stdmethod.kizu:8-219` | `TypeText` `ParamType` `Method`(+`substitute`)`MethodHandle` `MethodIndex`(+`find`)`index_methods` `index_function` `insert_method` `method_from_decl` `copy_text` `render_return` | 約 205 | Go の `methods.go` の移植。他 module からの `stdmethod::` 参照は `Access` `Container` `Facts` `container_*` `method_facts` `method_name` `split_method_name` だけ。`stdmethod_test.kizu` の対応 test も一緒に消す |
| `internal/stdmethod/stdmethod.kizu:240` | `call_name` | 6 | Go の `CallName` は生きているが Kizu 本体は呼ばない |
| `internal/stdprim/stdprim.kizu:631` | `typed_core_builtin` | 11 | Go の `TypedCoreBuiltins` は参照 0(§1 A)。対で消す |
| `internal/typ/typ.kizu:1633` `:568` | `substitute_text` `split_args` | 43 | 本体の呼び出しは同名の別定義(`ownership::NameTable.substitute_text`、`stdmeta` の private `split_args`)へ行く |
| `internal/types/types.kizu:578,1151` | `arg_texts` `single_generic_arg` | 26 | Go に対応なし |
| `internal/ast/ast.kizu:3105` | `construct_expansion` | 22 | Go に対応なし |

**B-2. Go 本体では使われ、Kizu では test からしか呼ばれない(移植差)**

消す前に、Kizu 本体が同じ検査を別の場所で持っているか(上の `is_public_type` と同じ形か)
を確かめる必要がある。持っていなければデッドではなく移植漏れ。

| 場所 | 定義 | 行 | Go 側の利用 |
| --- | --- | --- | --- |
| `internal/types/policy.kizu:8` | `reserved_function_static_param_index` | 18 | `types/checker.go:874` |
| `internal/types/type_params.kizu:86` | `enter_signature` | 23 | `types/checker.go:871,1508,7062` |
| `internal/types/types.kizu:743` | `join_types` | 22 | `types/checker.go:4899,7140,8561` |

**B-3. test の入口・assert 用(残す推奨)**

| 場所 | 定義 | 行 |
| --- | --- | --- |
| `internal/ir/lower.kizu:439` | `new_lowerer` | 16 |
| `internal/ownership/ownership.kizu:741` | `check_all` | 30 |
| `internal/types/check.kizu:54` | `check_all` | 25 |
| `internal/types/type_resolution.kizu:152` | `new_checker` | 3 |
| `internal/types/type_resolution.kizu:282,287,292` | `bind_type_arg_value` `copy_resolution_text` `meta_field_owner` | 15 |
| `internal/parser/parser.kizu:62` | `errors` | 17 |
| `internal/ast/ast.kizu:884,1034,1046,1578` | `add_empty_block` `render_statement` `render_block` `render_declaration` | 37 |
| `internal/diagnostic/diagnostic.kizu:568,573` | `source_span` `severity_level` | 10 |
| `internal/stdprim/stdprim.kizu:654` | `builtin_names` | 14 |
| `internal/stdmeta/stdmeta.kizu:259` | `names` | 12 |
| `internal/unsafecap/unsafecap.kizu:15` | `all` | 12 |

**B-4. 参照 0 だが文書の役割がある**

| 場所 | 定義 | 行 | 備考 |
| --- | --- | --- | --- |
| `internal/typ/typ.kizu:197-212` | contract `TypeVisitor` `NameMapper` `NameFolder` | 16 | contract は構造的で、`walk` / `map_names<M>` / `fold_names_into<F>` は型引数を contract で縛っていない。消しても検査は変わらないが、要求の形を書いた唯一の場所 |

## 3. std(`lib/kizu/std`)

### A: 安全に消せる(module private、参照 0)

| 場所 | 定義 | 行 | 備考 |
| --- | --- | --- | --- |
| `compress/deflate.kizu:88` | `tables_size` | 1 | `compress.kizu:251` の comment が名前を引いている。消すなら comment も直す |
| `float/float.kizu:26` | `fraction_bits` | 1 | 隣の `exponent_bias` などは使われている |
| `json/json.kizu:1830` | `deinit_elements` | 17 | 本体が引数に無い `allocator` を参照している。generic が一度も instantiate されないので検査を通っている |
| `tls/connection.kizu:46` | `alert_internal_error` | 3 | `spec/` にも出てこない。`std::tls` の状態機械には触れない(alert 番号の定義だけ) |

Go 側(`internal/stdlib` 等)は std を名前で引くが、これら 4 つの名前はどの Go ファイルにも
現れない。

### B: 要注意

| 場所 | 定義 | 行 | 備考 |
| --- | --- | --- | --- |
| `http/reader.kizu:198` | `(Reader).is_bounded` | 3 | 公開型 `std::http::Reader` の method だが `docs/std/http.md` に載っていない。公開 API かどうかの判断が要る |

### C: 公開 API(repo 内では未使用だが利用者向け)

いずれも `docs/std/` に載っている。repo 内で使っていないことは削除の理由にならない。

| module | 定義 | 備考 |
| --- | --- | --- |
| `std::crypto` | `hmac_sha384` `hmac_sha512` | |
| `std::io` | `read_stdin_into` | |
| `std::json` | `encode_with_tabs`(と `encoder_with_tabs`) | |
| `std::math` | `exp2` `log1p` `exp_with` `log_with` `pow_with` `acos` `atan` `cosh` `asinh` `atanh` | `cmd/kizu/math_test.go` が名前で program を生成して精度を検査している |
| `std::tls` | `(Connection).cipher_suite` | `tls_client_test.go` / `tls_server_test.go` が使う |
| `std::http` | `(Response).is_finished` | |
| `std::net` | `(TcpListener).clear_accept_deadline` `(Child).read_stderr_into` `(Poller).watch_child_stdin` | 後ろ 2 つは `SPEC.md` にも名前がある |

## 4. #1962(モジュール単位ビルド)由来の残骸

**見つからなかった。** #1962 は旧経路の関数を自分で消している(Go の `llvm.EmitNative`、
`moduleForLinker` / `runClang` の単一 IR 版、Kizu の `emit_native` ほか)。
残っている単一モジュール経路は次の 2 か所から今も呼ばれている。

| 経路 | Go | Kizu |
| --- | --- | --- |
| `--opt` の native build | `llvm.EmitUnits(…, split=!opt)` が `split == false` で `emit` を 1 回呼び、1 unit を返す。`native.unitInputs` は `Opt` のとき clang に text を直接渡す | `main.kizu:2453` の `llvm::emit_units(…, split, …)` |
| `kizu ir`(LLVM 出力) | `cmd/kizu/build.go:167` の `llvm.Emit` | `main.kizu:1685` の `llvm::emit` |

#1962 が足した `ir.directCallees` は `DirectCallees` を呼ぶだけの wrapper になった。
デッドではないが、畳める候補として記録しておく。

## 5. 削除する場合の PR の分け方

AGENTS.md の「両実装に同じ変更を入れる」に合わせ、Go と Kizu の対は同じ PR に入れる。
どの PR も挙動は変えないので、検証は `pre-commit run --all-files`(`kizu check compiler`
を含む)、`go test ./...`、`just selfhost` で足りる。

| 順 | PR | 内容 | 規模 |
| --- | --- | --- | --- |
| 1 | stdmethod 旧 method index | Go `methods.go:8-97`(A)+ Kizu `stdmethod.kizu` の型群・関数群と `call_name`(B-1)+ 対応する `stdmethod_test.kizu` の test | 約 300 行 + test |
| 2 | Go 単独の A と対 | `stdmeta.Names`、`isBufferType`、`TypedCoreBuiltins` + Kizu `typed_core_builtin` とその test | 約 35 行 + test |
| 3 | selfhost の A | §2 A の 20 件。module ごとに commit を分ける(ir / llvm / ownership / typ・types・wasm)。`keep_reachable_functions` は 6 と一緒にするなら外す | 257 行 |
| 4 | std の A | §3 A の 4 件と `compress.kizu:251` の comment | 22 行 |
| 5 | (判断後)test 専用 wrapper | `ir.Lower` `ownership.New` `types.New` `wasm.Emit` `wasm.Lower` `lsp.Complete` `lsp.InlayHints`、`KeepReachableFunctions` + Kizu `keep_reachable_functions`。test を `*ForTarget` 等へ書き換える | 約 50 行 + test 書き換え |

B-2(移植差)は削除 PR ではなく、Kizu 本体に同じ検査があるかの確認が先。
B-3、B-4、std の B / C は残す前提。
