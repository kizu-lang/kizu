# 言語と std の gap

Kizu で大きなものを書いたとき —— compiler の移植、std の module —— に見つかった
language / std の不足と、その場で使った局所解です。証拠の欄はどこで詰まったかを
指します。

これは要望の一覧ではなく、実際に書けなかったものの記録です。同じ壁に二度当たらない
ために残しています。仕様として決めるものは `SPEC.md`、判断の理由は `docs/adr/`、
着手中のものは GitHub Issues が持ちます。

| gap | 証拠 | 局所解 |
| --- | --- | --- |
| sequence literal(`[N]T{a, b}` / table / table-driven test)が無い | parser の precedence table(if 連鎖 23 行)、Go の table test 7 本が展開されている | 展開のまま |
| 文字列組立の std API が `append_*` だけ | parser の message helper 155 行、checker の `errorf` 315 箇所 | parser は `error_*` method に融合(parser.kizu) |
| struct field からの take / replace が無い | parser が cur/peek を持てず全 token を arena に保持。fmt の Builder から完成した `out` だけを取り出せない | parser は `Arena<Token>` + handle stream。fmt は完成時に出力を 1 回 copy し、Builder 全体を deinit |
| `orelse` の右辺に block を書けない | parser の early return | `if opt \|v\| {} else {}` に展開 |
| owner の `?T` は産出した場所で capture / orelse が必須 | parser test | 同上 |
| `[]u8` view は関数から返せず、`as_bytes()` は local binding に束縛必須 | loader の `module_name() -> []u8` 系 helper と fmt の `tokenSpelling(Token) -> string` が書けない | `&string::String` を返す / 渡し、callee 側で bind する。fmt は `append_token_spelling` に変える |
| comparator を受ける stable sort が無い(`std::sort` は owned String 専用) | fmt の `normalizeLeadingImports` は variable-length token range を `sort.SliceStable` で path key 順にする | Token owner を 1 Array に保ったまま、隣接 import range の stable insertion / rotation を局所実装 |
| alias import が無く、import は末尾 segment 名だけを束縛する | compiler main は既存の `std::fmt` と新しい `compiler::internal::fmt` の両方を使うため `fmt` が衝突する | 既存の decimal append 4 call だけを 5 行の `compiler::internal::stdfmt` wrapper 経由にする |
| error set を宣言しない `!T` は `try` 専用で `catch` できない | fmt の Go `insertMoveMarkers` は `loadFileProgram` の全 error で元 source を返すが、Kizu の `Loader.load_source -> !LoadResult` は load graph 内の fs / allocation failure を catch できない | `LoadResult::Error`、types diagnostic、ownership diagnostic は元 source のまま返す。診断になる前の fs / allocation failure だけは伝播し、project 全体の error set を移植途中で変更しない |
| current working directory を読む API が無い | init の Go `filepath.Abs(target)`。`TestSelfhostFrontend/init` の引数無し case は process の cwd を変えて比較する | `fsutil::absolute` は test / shell が cwd と同じ値に保つ `PWD` と `path::join` / `clean` を使う |
| file の exclusive create が無い | init の Go `writeNewFile` は `O_CREATE|O_EXCL` で事前検査後の race でも既存 file を上書きしない | `reject_existing_init_files` で `kizu.toml` と `src/main.kizu` を先に拒否してから `fs::write_file`。検査と write の間に別 process が作る race は残る |
| `match try f()` では owner payload を move out できない | loader の `match try self.read_std_graph()` | `let r = try f(); match r { ... }` |
| `else if` が無い | loader | `else { if ... }` に展開 |
| owner field への代入不可のため「field を入れ替える」書き方が無い | loader の `self.order` の差し替え、ModuleFile.imports の設定 | 空で作って in place に insert する。入れ替えが要る場合は 2 つの field を持つ |
| Array.at の結果は capture 限定で `orelse` も不可 | checker body port | `if arr.at(i) \|v\| {} else {}` |
| `typ::map_names` の rename callback は error set しか返せず diagnostic を運べない | loader の resolve_type_node | mapper struct に失敗を記録して呼び出し後に読む |
| view field を持つ struct(`ast::ConstructField { []u8, []u8 }`)を view binding から作れない | checker body の construct_expansion 呼び出し | builder(`begin_construct` / `add_field` / `finish_construct`)に変えた |
| `while` 本体で作った view が同 block の `defer owner.deinit(allocator)` と衝突する | checker body | view を iteration ごとに bind し直す |
| `mem::slice` を view に使えない / `?[]u8` を返す helper に view binding を渡せない | checker body | `x[a..b]` で切る / index を返す helper にする |
| union payload に `?T` を置けない | loader の qualify(optional child を持つ結果) | optional 子は inline で分岐 |
| `?Owner` を値で渡せず `&?T` 引数も不可 | loader の qualify(`copy_docs`) | capture した `&Map` を渡す。literal を 2 箇所に複製 |
| expression の match arm で `return` できない | loader / checker body | statement の match に包む |
| closure が無いので callback 型 API(`typ::map_names`)に Loader を渡せない(struct に借用を持てない) | loader の resolve_type_node | 2 pass(名前を集めて解決し、rename 表で map_names) |
| `std::process` は `argv[0]` すなわち実行ファイルの path を出さない | CLI の lib dir 探索(Go は binary 隣の `lib/kizu`) | `--lib-dir` / `KIZU_LIB_DIR` のみ。既定は `lib/kizu` |
| method の `&T` 戻り値は ownership checker が borrow として追跡しない(`calledFunction` が free function と namespace 修飾名しか解決せず、`let s = self.text(n); s.as_bytes()` が view 初期化子と認識されない) | ownership の `NameTable`: intern した綴りの bytes を読む accessor | free function `name_text(names: &NameTable, name) -> &string::String` にし、call site で `let retained = ...; let bytes = retained.as_bytes();` と束縛する |
| IR lowering が `&var` 引数の slot 判定を method 名だけで program 全体に union する(`internal/ir/slots.go` `markLentMethodArgs`)ため、別 module に同名で `&var` 位置が違う method があると capture / payload binding の lowering が壊れる(`clone`、`is_plain_data_type` で project / types の関数が clang error になった) | ownership module の `ScopeStore.clone` / `Checker.is_plain_data_type` ほか | 衝突した method だけ名前を変えた(`clone_scope`、`binding_mut`、`check_call`、`check_value_block`、`is_plain_data`、`check_owned_string_method` など)。同名 method の `&var` 位置差を列挙する確認を module 完了時に行う |
| `test` block の本体は同 module の private field を読めない | ownership の `binding_test.kizu`(`BindingHandle.node` の比較) | 比較を同 module の `fn` に出す |
| `if try f() \|v\|` の capture から `&var self` method を呼べないため、borrow を返す accessor(`?&T`)は使いにくい | ownership の struct / enum / union 表の読み出し | accessor は copy 値(`?Name`、`?BindingHandle`、`bool`)を返す形にし、`Map.at` は accessor 内部で閉じる |
| Go の `defer` による状態復帰(`c.loopDepth--`、flag の戻し)を `defer` で書けない(cleanup method 呼び出し以外は `defer` 不可)上、`?Owner` 戻り値を `let` に束縛できない | ownership の `check_block` / `check_while_stmt` / generic instantiation の enter / restore | 失敗 branch と成功 path の両方に復帰を書く(`if try f() \|failed\| { restore; return move failed; } restore;`) |
| union の payload に `Allocator` / `Io` を持つ struct を置けない(llvm の inline payload layout #991 が `Allocator` を知らない) | ir の `LowerResult { Module(Module), Error(Diagnostic) }`(`Module` は `NameTable` の allocator を持つ) | `Lowerer` が `pub module` を持ち、caller が `Lowerer` を生かしたまま `&lowerer.module` を読む(Go の `Lower() (*Module, error)` の戻りを in place にした) |
| 値受け `self` の method(`fn (self: T) into_x() -> X`)は receiver を consume しない(consume になるのは `deinit` だけ)。`deinit` を宣言した型の field は move out できない | ir の `Lowerer` から `Module` を取り出す | `Lowerer` に `deinit` を宣言せず(導出)、module は取り出さず上の形にした |
| method 名だけで `&var` 位置を union する slot 判定(既出)の新しい衝突: `set`(`EnvStore.set` と `Array.set`)、`clone`(`EnvStore.clone` と `Array.clone`)、`resolve_type`(ir と project) | `project::Loader.qualify_decl` の match payload `value.tags.clone(..)` が slot 化され clang error | `bind` / `unbind` / `clone_env` / `resolve_bound_type` に改名。module 完了時に全 method 名の `&var` 位置差を列挙し、ir が増やした差は `tree` 引数の位置(常に parameter を渡すので slot 化しない)だけであることを確認した |
| `sync.Once` の process 内 cache(`project.StdErrorSets`)に当たるものが無い(shared mutable global を置かない) | ir の error set 番号付け | `project::Loader.std_error_codes()` を呼び出し側(CLI / corpus runner)が 1 回読み、`&StdErrorCodes` を lowering に渡す |
| `typ::Table.parse` の失敗は `!` で伝播し、Go の「parse できなければ text のまま」に当たる optional parse が無い | ir の `lowerReturnType` / `errorUnionParts` / `resolveMetaTypeDeep` | 空 text だけ guard し、checked program の spelling は parse できる前提にした |
| `ownership.Result` を値で次の phase へ渡せない(`Name` が Checker の `NameTable` に tied) | ir の `Lower(program, ownershipResult)` | `Checker` を caller が生かし、`retired_return_at` / `retired_try_at` / `retired_name_text` で読んで lowerer 側の `NameTable` へ copy する |
| `if try f(view) \|x\| { ... }` は capture の間 condition の借用が生きるため、body で `&var self` を呼べない | ir の `split_static_args` / `generic_bindings` | text を local `String` に copy してから split する |
| `&var ?T` の parameter を書けず、後の file で宣言される型の `?T` field も置けない(struct の field 検査が file 順) | ir の control.kizu(`lower_loop_header` の index phi) | union `LoopTest { Plain(Value), Indexed(cond, phi) }` で header の結果を運ぶ |
| `if`/`match` は statement と expression で別 node なので、値位置の `if`/`match` を statement として歩けない | ir の `statementValue` / `collectAssigned` / slot walk | `TrailingValue` union と、値位置専用の walk(`collect_assigned_if` / `collect_mut_borrows_value_stmt`) |
| `Map` を空にできず(`clear` が無い)、owner field の差し替えもできないので、Go が関数ごとに作り直す `map[string]T` を写せない | llvm の `values` / `blockExitLabel`(`writeFunction` が `= map{}` で作り直す) | entry に関数の generation 番号を持たせ、違う generation の entry を無いものとして読む |
| `Array.clear` / `truncate` は std 専用 method で user code から呼べない | llvm verify の `uses = nil`、corpus runner が std 関数を module から落とす処理 | `while values.pop() \|v\| { v.deinit(allocator); }` で空にする。module の `functions` は全部 pop して user 関数だけ append し直す |
| 同じ式の中で `&var self.out` と `&self.names` を同時に渡せない(`self` 全体が借用済みになる)。逐次の文なら disjoint field の借用は通る | llvm の `line*`(`format_args(&var self.out, &self.names, ...)`) | `let names = &self.names;` に束縛してから `format_args(&var self.out, names, ...)` |
| 引数式の中で `&var self` method を入れ子に呼べない(`self.line2(fmt, Arg::Str(try self.own(...)))` は receiver の借用と衝突) | llvm の全 writer | 引数を先に `let` に束縛する |
| 関数が返した view(`deref_llvm_type(bytes)` の結果)をそのまま別の呼び出しの引数に置けない(escape 扱い)。`let x = f(view)` に束縛すれば通る。`let x = f(view) orelse ...` も escape 扱い | llvm の `takes_address_of` / `write_optional_types` | 束縛してから渡す。`orelse` の代わりに `if f(view) \|x\| {}` |
| union は copy payload だけでも move-only で、`!` を返す関数に渡した union 引数は `errdefer arg.deinit(allocator)` を要求する | llvm の `Arg` union(`fmt.Fprintf` の引数) | formatter は引数を 1 つずつ `place_arg(... move arg)` に渡し、各 helper は `errdefer a.deinit(allocator);` を並べる |
| union payload / struct field に local binding 由来の `[]u8` view を置けない(literal と literal 由来の戻り値だけ) | llvm の `Arg::Lit`(Go の string 引数) | 生成 text は `Name` に intern(`Arg::Str`)か owned `String`(`Arg::Owned`)で渡し、`[]u8` を返す helper(`llvm_binary_op` など)は `&NameTable` + `Name` を受けて literal だけ返す |
| closure / function value が無いので callback を取る Go 関数(`collectModuleTypeNames(collect func)`、`writeContainerNew(isResultType func)`)を写せない | llvm の header / container | 呼び分けを enum(`TypeCollector`、`ContainerKind`)で受け、中で match する |
| `typ::walk` の visitor は `&Node` しか受けず、部分木を render する `Type` handle を持てない | llvm の `collectErrorUnionName`(`typ.Walk` で ErrorUnion node を `String()` する) | `root_node` / `child_type` で明示的に再帰する |
| 子 process の出力 capture が無い(`spawn_wait8` は stdout / stderr 継承) | native の `runClang` / `compileRuntime`: Go は clang の CombinedOutput を成功時は捨て、失敗時は error に載せる。selfhost では clang の `-Woverride-module` warning が毎回 stderr に流れ、失敗出力を message に運べない | 失敗 message は「native error: clang failed: exit status N」の行だけ合わせる。`TestSelfhostBehavior` / `TestSelfhostFrontend` は selfhost 側 stderr から toolchain noise を落とし、Go の build が clang で失敗する case は比較から除外 |
| fs に `os.MkdirTemp` / `RemoveAll` / `MkdirAll` 相当が無い | native の一時 build directory 管理 | `TempDirs`(TMPDIR + `kizu-native-<monotonic_millis>-<連番>`)と `create_dir_all` / `clean_build_dir`(既知 file の削除 + rmdir)を module 内に書いた。buildcache module でも要るなら std gap として切り出す |
| `spawn_wait8` は引数 8 個の固定形 | clang の link argv は `--triple` 付きでちょうど 8。`run` の child args は exe + 7 個まで | clang は triple 有無で 2 つの呼び出し形に分岐。8 個を超える構成が要る場合は止めて報告する |
| `std::json` は `<` `>` `&` を `\u003c` 形に escape しない(encoding/json の HTML escape 差) | native の build metadata(`write_metadata`) | metadata の値にこれらが入るのは path だけで、`TestSelfhostFrontend` は絶対 path を正規化して比較する |
| generic 関数の呼び出しは static 引数の明示が必須(推論されない) | native の `sorted_keys<V>` | call site で `sorted_keys<i64>(...)` と書く |
| `runtime.GOOS` / `GOARCH` 相当が無い | native の toolchainKey(cache key の host 部) | claim した temp dir で `sh -c "PATH=/usr/bin:/bin uname -sm > file"` を実行して読む(spawn の出力 capture 不能の既知 gap の続き)。未知の host は guess せず error で止める |
| `os.CreateTemp` 相当が無い(乱数源も無い) | buildcache の scratch file(半端な artifact を key の名で見せないための build 先) | `fsutil::append_scratch_name`: `artifact-<key 先頭 16 桁>-<unix_millis>-<連番>`。並列プロセス間の distinctness は build 中の key が持ち、同じ key を同じ ms に 2 プロセスが build した場合だけ衝突して負けた側は明示的に失敗する。rename 前に fs error で止まると scratch が残る(Go は defer Remove が拾う) |
| `strings` の走査 API が無い(`Fields` / `Split` / `Join` / `Contains` / `HasPrefix` / `Index` / `Count` / `ReplaceAll` / `TrimSpace`)と、`unicode.IsSpace` に当たる述語も無い | cimport は Go 196 行に対し 179 行をこの代替に使う。fmt は `strings.Split` / `ContainsAny` / `TrimSpace`、llvm は `strings.Contains` 系を、それぞれ module 内に別々に持っている | module 内に局所実装。空白判定は `space_width` が White_Space の 25 scalar を byte 幅で読む(UTF-8 の continuation byte は lead byte になれないので byte 走査で誤検出しない) |
| `std::fs` の error は失敗した path も read 段階の errno も運ばない | CLI の全 command が target を読む。Go は `open <path>: no such file or directory` / `read <path>: is a directory` を出す | std は変えずに CLI 側で解いた。path は caller が持っているので `read_source_file` が `fs::Error` を捕まえて message を組む。runtime が read 段階の errno を `ReadFailed` に畳むので、directory かどうかは `fs::metadata` に聞き直す |
| `strconv.Unquote` / `strconv.Atoi` に当たる std API が無い | wasm は quoted IR literal を読むのに llvm と同じ `append_unquoted` を、`strconv.Atoi` の受理条件(符号 + 数字 1 つ以上 + int64 に収まる)を `is_decimal` を、module 内にそれぞれ持つ | module 内に局所実装。llvm / fmt / cimport が持つ同種の helper と一緒に module 完了時に判断する |
| `time` package 相当が無い(civil 変換・RFC3339・時刻比較) | buildcache の `Entry.CreatedAt`(Go は time.Time の JSON) | `compiler::internal::timestamp`(別記録): 書きは `std::process::unix_millis()` から civil 変換で RFC3339 UTC(fraction は RFC3339Nano と同じく trim)、eviction 順序は spelling の桁比較 `stamp_before`。offset 付き stamp は 0 扱い(両 CLI は Z しか書かない) |
| 関数 pointer 型 `fn(...) -> T` は borrow parameter を運べない(call site で `&x` が `x` として型検査され、`argument N expects &T, got T` になる)。`Function` static parameter は型検査だけあって lowering が無く、呼べない | std::http の server 設計。Go / Zig は handler を取るが、Kizu では handler に `&Request` を渡せない | server を pull の loop にした(`accept` が Exchange を返し、caller が loop を書く)。routing も同じ理由で登録簿ではなく `route` への質問 |
| `[]u8 == []u8` / `!=` は型検査を通るが LLVM に降りない(`icmp requires integer operands` で clang が落ちる) | examples/http_client を書いていて `host != ""` で踏んだ | `mem::equal_bytes` / `mem::len` を使う。診断が compiler から出るべきで、clang から出るのは傷 |
| `[]u8` を返す関数の結果は式のまま次の呼び出しに渡せない(`print(path_of(target))` が borrow escape) | std::http の `path_of` / `query_of` を使う全ての例 | `let path = path_of(target);` に束縛してから渡す |
