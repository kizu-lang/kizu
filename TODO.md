# TODO

ここには未完了の実装だけを置きます。番号は優先順ではなく識別子です。完了したものは
削除し、現在の仕様は `SPEC.md` / `docs/`、経緯は ADR と git log が持ちます。

## メモリ安全監査の修正

2026-09 の監査で、`kizu check` が通すのに実行で use-after-free / double free になる
経路が 11 系統見つかった。各経路は `examples/negative/` と `examples/` に
`pending:` 付きの case として置いてあり、`grep -rl "^// pending:" examples` が
残作業の一覧になる。直した change で `pending:` 行を消す(conformance test が
「passes now」で強制する)。

原因の 9 割は「ある構文の形で実装した規則が、隣の形に無い」こと(while/for、
defer/errdefer、ident/slice、非 generic/generic、`?T`/`E!T`、`String.deinit`/
`Array.deinit`)。個別に塞いだ後、checker を形ではなく値の階級と署名で規則を
持つ構造に畳む(`docs/principles.md` §11)。

手順は上から順。Go を直してから `compiler/` へ機械移植し(ADR-0130)、
diagnostics の parity は `compiler/tests/check` の corpus に `neg_*` を足して
`go test ./internal/types -update` で固定する。

### C. SPEC 追記と、共通部品へ畳みながら閉じる修正(中)

- [x] C3 呼び出しを 1 本にする: 直接 / generic / method / fn pointer / std method が
      「subst 済み署名 + 引数列(receiver は第 0 引数)」を受ける同じ関数を通る。
      generic の owner 引数の move、`&var`+`&` alias、view の lend、tie 導出、
      `append_string` の receiver alias がここで閉じる
      (`generic_call_reads_owner`, `generic_call_move_marker`,
      `generic_call_aliases_receiver`, `generic_return_carries_view_tie`,
      `generic_call_lends_view_into_container`, `std_string_append_string_self`)
- [x] C4 式の結果を `{型, 階級, tie 集合}` にして全式が伝播する。slice / field 読み /
      `.*` が special case でなくなる(`slice_view_keeps_tie`, `slice_view_escape`)
- [x] C5 loop を `loopRegion(条件式列, body)` 1 本に、cleanup を
      `registerCleanup(kind, binding)` 1 本に畳む(A1〜A4 をここで消化しても
      よい)
- [x] C6 std method の receiver 種別 / 引数の扱い / 戻り値の tie を
      `internal/stdmethod` のデータにし、checker の `case "append_string":` 類を消す

### 残った小さいもの

- [x] `defer` / `errdefer` の中の使用を block 末尾の使用として数える
      (`defer_keeps_allocator_tie`)。
- [ ] `std::internal::builtin::*` primitive(std 内部だけが呼ぶ)の `case` 分岐は
      残っている。利用者が通る method は全て `checkStdMethodCall` を通る。

### 決めが要るもの

- [x] `orelse` / `catch` の右辺(既定値)で名前付き owner を消費できないようにする。
      決めた: 既定値は payload が無いときだけ走るので、右辺での hand-off
      (`orelse move keep`、`orelse wrap(move keep)`)や消費 receiver
      (`orelse boxed.take(a)`)は条件付きの消費になり、checker が無条件 move と
      扱って leak していた(実測 allocs 2 / frees 1)。隠れた解放は入れない
      (解放は allocator を名指しする、ADR-0132)。右辺でその場に作る owner
      (`orelse string::new(a)`)は評価されない path で作られないので今まで通り。
      作業: 右辺評価中の flag で hand-off / 消費 receiver を拒否、negative 3 件
      (`orelse move` / `catch move` / consuming receiver)、SPEC §6.9.1 に 1 行、
      Kizu 移植。既存コードの書き換えは 0 件。
- [ ] (別判断) capture 付き `if` を式として許すか。用意した owner を条件付きで
      使う形を `let picked = if try maybe(a) |found| { keep.deinit(a); found }
      else { move keep };` と式で書けるようになる。今は文形だけ。
- [x] std container の method 呼び出しも ADR-0106 の two-phase receiver を通す。
      決めた: user method と経路を 1 本にする(`checkMethodArgs` の `reserve`
      flag を消す)。`arr.append(a, arr.pop_or_panic())` は
      `Array.pop_or_panic cannot run while array is borrowed`。compiler の
      書き換えは lexer の 1 箇所(`std_array_two_phase_receiver`)。
- [x] `extern "c" fn` の引数と戻り値を C が名指しできる型に限る(整数 / 浮動小数 /
      `bool` / raw pointer / `void`)。決めた: `[]u8`、borrow、owner、struct、
      error union は拒否し、SPEC §12 に規則を書いた。診断は本文 + note(C が
      受け取れる型)+ help(`ptr<const u8>` と `usize` で渡す)。negative 3 件
      (`extern_c_view_param` / `extern_c_borrow_param` / `extern_c_view_return`)。

## std::http / std::net の残り

evented server(ADR-0136〜0146)まで入った時点で残っているものです。

## 2. 抱える数の上限 (#1083)

TaskSet の visible accept loop は connection を無制限に accept / spawn できる。
実測では worker が約 269 KiB/connection を使うため、`max_requests` では代わりに
ならない。max connections / max in-flight の数え方、上限時に accept を待つか明示的に
断るか、完了 worker を caller がどう観測するかを #1083 で決める。

serve loop 自体は `first` / `next` で入った(ADR-0144)。`serve` は作らず、loop は
caller のもの。停止は `break`。期限の掃除は `next` の中なので、書かなくても塞がる。

## 3. protocol の穴 (#1082)

| | 大きさ | 備考 |
| --- | --- | --- |
| trailer を header に足す | 小 | 今は消費して捨てる |
| upgrade (101 / WebSocket) | 小 | `Framing::Raw` は既にある |
| pipelining | 中 | 先読みは順に処理、答えは重ねない |
| multipart / form-data | 中 | |
| compression | 大 | 圧縮 library が要る。別の話 |
| `Date` header | — | std に暦が無い。暦が先 |
| HTTP/2 / HTTP/3 | 大 | |

## 4. TLS / HTTPS (#1081)

未着手。provider 境界の設計から。独立していていつでも始められる代わりに
一番大きい。

## 5. middleware (#1085)

pattern routing (`route.kizu`) は入った。関数 pointer は borrow parameter を運べる。
closure は無いので、状態を明示引数にする composition と、呼び出しを隠す登録簿の
どちらを middleware と呼ぶかを #1085 で決める。
