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
- [ ] C6 std method の receiver 種別 / 引数の扱い / 戻り値の tie を
      `internal/stdmethod` のデータにし、checker の `case "append_string":` 類を消す

### 決めが要るもの

- `let r = f() catch move fallback;` / `orelse move fallback;` の非採用 path で
  fallback が leak する。named owner を fallback に置けなくするか、非採用 path で
  解放するかを決める
- std `Array` の two-phase receiver: `arr.append(a, arr.pop_or_panic())` が通る。
  ADR-0106 は receiver を借りる引数を拒否する。bounds check のおかげで今は安全
- `extern "c" fn` が `[]u8` / `&var String` を引数に取れる。SPEC §12.1 は `[]u8` を
  browser ABI 限定としている

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
