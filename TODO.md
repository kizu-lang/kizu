# TODO: std::http / std::net の残り

PR #1708 (`f7fa84d9`) まで main に入った時点での残り。Issue には分けず、
このファイルを作業の正本にする。番号は優先順ではなく識別子。

## 0. evented HTTP server の残り

### 0a. head を読まずに park する accept  ← 完了

- `server.accept_connection(io, allocator)` は接続まで待ち、request を読まずに
  `Exchange` を返す
- `exchange.read_head(io, allocator)` が worker 側で普通に head を読む。false は
  head より前に peer が閉じたこと
- accept loop は Exchange を TaskSet へ move してすぐ次の accept に戻る
- head deadline / malformed request の拒否 / Exchange cleanup は従来と同じ

### 0b. N 個の Future と N 個の state を並べて持つ  ← 完了

- `Future` は結果を読み戻す 1 state を借りる形のまま
- `TaskSet` は渡し切る N state を所有する。`spawn` が struct state を worker へ
  move し、完了時は自動回収、`TaskSet.deinit(allocator)` は残りを cancel する
- Io / allocator は `task_set_new(io, allocator)` で 1 回固定し、TaskSet の lifetime
  を両 capability に tie する。state は view / Io / Allocator を含められない
- `examples/http_tasks.kizu` が production shape、behavior test は quiet な 1 接続が
  head 待ちでも後続の完全な request が応答されることを確認する
- 判断は既存 ADR-0146 を更新。新しい ADR は増やさない

### 0c. worker stack overflow を安全に止める  ← 完了

- `io::async` / `io::spawn` の caller から `stack_bytes` は削除した。backend が決める
  frame size を caller は導けないため、std が 256 KiB を一箇所で持つ
- worker 実行中には伸ばさない。確保失敗を見えている `async` / `spawn` から
  `OutOfMemory` として返し、live borrow を含む stack を移動しない
- `Future` も `TaskSet` と同じく指定 allocator から header と stack を取り、
  `deinit(allocator)` が同じ allocator を名指して返す。貸された state は排他的、
  保持する Io / allocator は shared に tie する
- allocator から usable stack + 2 page を 1 block で取り、中の 1 page を
  境界に揃えて `PROT_NONE` にする。全 native Kizu 関数は 4096 byte 以下の
  間隔で stack を probe するため、大きな frame も guard を飛び越せない
- guard の設定に失敗した spawn は `StackProtectionFailed` を返し、保護なしで
  実行する fallback は持たない。実行中の overflow は catch や unwind をせず、
  領域外へ書く前に OS の保護違反で process を止める
- `std::coro::spawn` は低レベル API なので byte 数を明示する形を保つが、同じ
  overflow 保護を通す
- `examples/negative/coro_stack_overflow.kizu` が guard より大きい再帰 frame で
  process が停止することを確認する

### 0d. `max_requests` の既定を再検討する

- 現在値 1 は変えず、TaskSet server で connection ごとの memory、idle timeout、
  fairness と keep-alive の throughput を測ってから決める

## 1. body を読み出し口にして、二重の読み取りを畳む  ← 完了

`feat/a-poller-is-a-loop-that-waits-on-many` の 6 commit で入った。

| | commit |
| --- | --- |
| 1 | `7f2c56b8` body は保持するものではなく読むもの(server + 畳み込み) |
| 2 | `b9342f30` client も同じ形。`Reader` を両側が持つ |
| 3 | `4d43431f` (途中で見つけた flake の修正) |
| 4 | `ace2cd5f` 黙っている接続が時間切れになる |
| 5 | `1949d20a` 接続は返ってくる。返さなければ次は来ない(項目 3 も込み) |

決定は ADR-0143(body は読むもの)と ADR-0144(接続の手渡し)にある。

### 途中で覆した判断

- **決定 13(`serve<handler>`)は書けなかった。** `Function` static parameter は
  Kizu の body から呼べない(`worker(x)` は `undefined function`)。`first` /
  `next` の手渡しに変えた
- **`serve` も `stop` も作らなかった。** loop は caller のもので、止めるのは
  `break`。単一 thread で signal も無い今、`stop()` を呼べる場所は loop の中だけ

## 2. evented への道 (#1084)  ← 完了

| 段 | | 状態 |
| --- | --- | --- |
| A | `Function` static parameter を Kizu の body から呼べるようにする | **完了** (`29073a72`)。両実装。**使い手はまだ無い** —— C で要る見込み |
| B | stackful coroutine の runtime | **完了** (`29073a72`)。`std::coro`、ADR-0145 |
| C | `Io.async` / `Future` の形 | **完了**。ADR-0146 |
| D | `io::evented()` | **完了**。ADR-0146 |

ADR-0141 が書いた順番(中断できる runtime が先、`Io` がそれを表すのが次、
`evented` はその実装)に従い、3 つとも入った。

### C / D で決めたこと

- **worker は `fn(Io, Allocator, &var A) -> void`。** closure が無いので capture
  は無く、Io と allocator は引数。作業対象は caller が**貸す** 1 つの値
- **`Future` は状態を持たない。** user 定義の generic struct を構築できない
  (`field Pair.first expects T, got i64`)ので `Future<A>` が書けなかった。
  貸す形にして、Future を貸した値に tie した
- **`blocking()` の `async` はその場で走らせる。** 走らせるものが他に無い実装に
  とってはそれが正直。Zig の `Io` も同じ
- **park した coroutine は loop が起こす。** `kizu_net_wait` が 1 箇所なので、
  evented な Io のときだけそこで poll の代わりに park する
- **cancel は待ちに届く失敗**(`net::Error::Canceled`)。worker は自分の
  `catch` / `defer` を通って戻る

### C / D で分かったこと

- tie 検査は `Allocator` 専用ではなく capability の規則だった。`Io` を足すのに
  述語 1 つ(`capabilityReturn`)で済んだ
- tie 済み capability を**引数として貸す**経路が method 呼び出しに無かった
  (`checkImplMethodArg` に `viewArgLend` しか無い)。tied allocator でも同じ穴
- `try f(...)` の結果は tie recognizer に届いていなかった(`TryExpr` を
  `CallExpr` として見ていない)。`!` を返す factory は tie されなかった
- `stdprim` の builtin 名一覧が lexical order を名乗って coro を末尾に足して
  いた。Go の `BuiltinNames()` から生成し直した

### A / B で分かったこと

- `genericBindings` が `Function` 引数を既に束縛していた。呼び出しに使って
  いなかっただけ
- `Function` 引数を持つ body は**インスタンス化のときだけ**型検査する。
  parameter が関数を名指すので、束縛されるまで body に型が無い
- `Function` static parameter は std 専用のまま(policy 不変)
- **関数 pointer は既にあった**(SPEC §5)。coroutine の entry はそれで足りたので、
  A は結局 B に要らなかった
- test block の中から関数 pointer を値として使えない bug を踏んで直した
  (合成名が module を持っていなかった)

## 2b. 関数 pointer が borrow を運べない  ← 別件、忘れないこと

```kizu
fn drive(worker: fn(&string::String) -> i64, held: &string::String) -> i64 {
    return worker(held);      // error: expects &String, got String
}
```

pointer 型は `&String` を正しく持っているのに、借用済みの引数が borrow と
認識されない。**SPEC §5 が「関数 pointer は safe に呼べる」と言っている機能の穴。**

原因: `internal/types/checker.go` の `checkFuncPointerCall` が、直接呼び出しの
borrow 処理(`requireMutableBorrowArg` / `coerceReturnedBorrowArgument`)を
通っていない。selfhost にも同じ穴があるはず。

直せば middleware (#1085) の設計余地が広がる。`evented` とは独立。

## 3. serve loop と shutdown (#1083)  ← 完了

`first` / `next` が入った(`1949d20a`、ADR-0144)。`serve` は作らず、loop は
caller のまま。停止は `break`。期限の掃除は `next` の中なので、書かなくても
塞がっている。

残るのは max connections / max in-flight —— どちらも「何本まで抱えるか」で、
抱えたものを捨てる規則が要る。まだ誰も困っていないので保留。

## 4. protocol の穴 (#1082)

| | 大きさ | 備考 |
| --- | --- | --- |
| trailer を header に足す | 小 | 今は消費して捨てる |
| upgrade (101 / WebSocket) | 小 | `Framing::Raw` は既にある |
| pipelining | 中 | 先読みは順に処理、答えは重ねない |
| compression | 大 | 圧縮 library が要る。別の話 |
| multipart / form-data | 中 | |
| `Date` header | — | std に暦が無い。暦が先 |
| HTTP/2 / HTTP/3 | 大 | |

## 5. TLS / HTTPS (#1081)

未着手。provider 境界の設計から。独立していていつでも始められる代わりに
一番大きい。

## 6. middleware (#1085)

pattern routing (`route.kizu`) は入った。**middleware は closure /
function value が無いので設計を固定できない。言語待ち。**

---

# 完了: Map と Arena も header そのものにする

(以下は完了済みの記録。段 A / A' / B / C / D すべて完了)
ADR-0131 が `Array<T>` に出した答えを `Map` / `Arena` にも適用する。
今の 2 つは header への pointer なので:

- 空の map / arena が 1 回 allocation を要求する
- `map::new` / `arena::new` が確保するのに失敗を言えない
  (runtime が NULL を返し、後で無関係な行で `kizu_panic_arena_add` が落ちる)
- header が `allocator` と `value_size` / `elem_size` を持つ(ADR-0132 の逆)

header を値にすると `new` は何も確保しないので `!` が要らず、失敗は
`insert` / `add` の `!` に畳まれる(zig の `HashMap.init` と同じ答え)。

## 段

### 段 A: Arena (完了)

- [x] A1. runtime.c: `KizuArena` と `kizu_arena_*` を全て削除。arena は
      `KizuArray` そのものなので array の op を使う (-88 行)
- [x] A2. Go backend: `Arena<T>` は `%kizu.array`、`arena.new` は
      zeroinitializer、`arena.add` は array append + 直前の len が handle
- [x] A3. ir lower: `add` / `deinit` の allocator 引数、`at_header`、
      `arenaPrimitiveParams`、slot 解析
- [x] A4. types / ownership: `arena.add(allocator, value)`、`Arena.deinit`
      primitive の allocator
- [x] A5. 呼び出し側 302 箇所 + Go test 32 箇所
- [x] A6. selfhost mirror
- [x] A7. corpus regen (`go test ./internal/{llvm,ir,types,parser} -run Corpus -update`)

### 段 B: Map (完了)

- [x] B1. runtime.c: `KizuMap` から `allocator` / `value_size` を落とし、
      5 word の header に。`kizu_map_new` を削除
- [x] B2. Go backend: `%kizu.map` header type、`map.new` は zeroinitializer、
      `map.len` は header 1 field の load
- [x] B3. ir / types / ownership: `insert` の allocator、map.kizu が
      `&var self` / `&self`、`mapPrimitiveParams`
- [x] B4. 呼び出し側 379 箇所 + 呼び出し元に `allocator` param を 134 箇所
- [x] B5. selfhost mirror / corpus regen

### 段 C: 仕上げ (完了)

- [x] C1. SPEC §10 / §14.3、docs/std/{map,arena}.md、ADR-0131 の書き換え
      (`0131-a-container-is-its-header.md` に改題)、ADR-0132 の decision 一覧

### 段 A': owner の borrow は address で渡る (完了)

段 A の途中で見つけた傷。`&T` parameter は copy で渡っていた
(`borrowIRType` が struct に対して `PassValue`)。なので

```kizu
b.show(b.put(allocator, 7));   // show: &self, put: &var self
```

は receiver の copy を先に取ってから argument が `b` を書き換えるので、
show が読むのは古い header。container が pointer だった頃は copy でも
同じ heap を指していたので無害だったが、ADR-0131 で Array を値にした
時点で開いた穴。selfhost が実際にこれで trap した。

原理 9 (経路は 1 本) から見ると `&T` が copy にも address にもなるのが
傷なので、**owner の borrow は必ず address** にした
(`ast.OwnerType` が持つ 1 つの定義に従う)。copy data は今まで通り値で渡る。

- [x] A'1. `borrowIRType` / `borrow_passing`: owner は `PassCopyAddress`
- [x] A'2. 既に address で届く parameter を slot で包み直さない
      (`&var &T` という borrow の borrow を作らない)
- [x] A'3. selfhost mirror / corpus regen

## 計測

| | main | 段 A 後 | 段 A' 後 | 段 B 後 |
| --- | --- | --- | --- | --- |
| selfhost binary | 15.5MB | 15.5MB | **11.1MB** | 11.7MB |
| compiler self-check peak RSS | 497MB | 489MB | **465MB** | 464-478MB |
| compiler self-check 時間 | | 3.91s | 3.70s | **3.35s** |

数字が動いたのは段 A' (struct の byval copy が消えた) だけ。
段 A / 段 B の memory の勝ちはほぼ無い —— arena は compiler 全体で 15 本、
map header は 500k 本作られるが短命なので peak には出ない。
段 A / 段 B の意味は「`new` が失敗を隠さなくなった」ことと、
runtime から arena の実装が丸ごと消えた (-88 行) こと。

### 段 D: 確保側の tie 検査 (完了)

段 B の途中で見つけた傷。ADR-0132 は「解放に渡す Allocator は owner を作った
ものと同じ」と書いているが、実装は `deinit` しか見ていなかった。

```kizu
var xs = array::new<i64>(heap);
defer xs.deinit(heap);
try xs.append(scratch, 7);       // check: ok、実行すると SIGABRT
```

これは main に既にある傷で、Map を header にしたことで `insert` にも
広がるところだった。

- [x] D1. `readGrowAllocator` が `checkReleaseTie` を通る
      (`Array.append` / `append_bytes` / `reserve`、`String` の 4 つ、
      `Map.insert`、`Arena.add`)
- [x] D2. tie の同一性を pointer / handle ではなく binding の **id** で見る。
      branch / loop body は scope の clone に対して検査されるので、
      pointer は一致しない (loop 内の append が false positive になった)
- [x] D3. selfhost mirror、`examples/negative/growth_names_another_allocator.kizu`、
      SPEC §14.3、ADR-0132 の tie 規則

## 判断の記録

段 B で足した 134 の `allocator: Allocator` param のうち 67 は receiver が既に
`self.allocator` を持つ method。一度は `self.allocator` に戻す案を出したが、
**引数のままが正しい**と結論。capability は渡されて初めて行使できるもので、
`self.allocator` を読む形だと「この method は確保するのか」が署名に出ない
(原理 2 / 原理 4)。「引数だと別の allocator を渡せる」という反論は段 D で
検査を入れたので消えた。
