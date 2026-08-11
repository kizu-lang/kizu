# selfhost backend 一般化の台帳

ADR-0080 の運用ドキュメント。selfhost backend(`selfhost/src/backend/`)を
「形状の列挙」から「一般 lowering」へ移行する作業の、棚卸しと進捗記録。

原則(ADR-0080):

- 新しい形状 lowering・関数名分岐は追加しない。
- 一般 lowering が扱えないソースに出会ったら、ソースを書き下げるのではなく
  ここに gap として記録し、一般化 backlog にする。
- shapes の退役数が進捗指標。退役 = 一般経路への置換 + 専用コードの削除 + 該当
  pin の挙動テスト化。

## 1. 関数名決め打ちの interception(compiled_llvm.kizu の dispatch)

2026-08-11 時点の棚卸し。各行が「この関数は一般経路を通らず専用 lowering で
描画される」ことを意味する。退役したら Status を更新し行を残す。

| # | 対象関数 | Status |
| --- | --- | --- |
| 1 | std::kizu::lexer::advance_position | active |
| 2 | std::kizu::lexer::first_token | active |
| 3 | std::kizu::lexer::is_doc_comment_start | active |
| 4 | std::kizu::lexer::next_token | active |
| 5 | std::kizu::lexer::number_token | active |
| 6 | std::kizu::lexer::raw_token_at | active |
| 7 | std::kizu::lexer::skip_line_comment | active |
| 8 | std::kizu::lexer::string_token | active |
| 9 | std::kizu::lexer::token_at | active |
| 10 | std::kizu::lexer::word_token | active |
| 11 | std::kizu::parser::is_namespace_path_span | active |
| 12 | std::kizu::parser::is_struct_literal_start(pps template) | active |
| 13 | std::kizu::parser::is_struct_literal_type_span | active |
| 14 | std::mem::trim_ascii | active |
| 15 | selfhost::ir::codegen::ast_node_text | active |
| 16 | selfhost::ir::codegen::string_literal_span | active |
| — | std::kizu::parser::is_type_apply_start(pps template) | **retired 2026-08-11(PR #1492)**: ソースが template の形を越えて成長し、template が意味論を黙って落としていた。一般経路へ移行 |

count_range(shape 判定つき)もこの dispatch にあり、形状族として §2 で扱う。

## 2. 形状 lowering の族(compiled_mir_lower.kizu)

個数でなく族で記録する(1 族に複数の変種と数百〜数千行が対応する)。

| 族 | 概要 | Status |
| --- | --- | --- |
| bounded counter while(MirWhileStmt) | induction counter + 定数 step / try-call / plain-call / field-read の latch 変種 | active |
| value-cursor while(embedded / with_init) | struct cursor を latch call で reseat する append ループ | active |
| dual-cursor loop | value cursor + scalar cursor の二重 carry(関数全体 shape) | active |
| trailing-token loop | Token cursor の advance ループ + 後続文 | active |
| precedence loop | パーサの優先順位 climb ループ | active |
| continue-latch while | body 内 if に increment を持つループ | active |
| count_range | 再帰 AST 集計ループ | active |
| generic while(lower_while_statement) | **一般経路**。environment ベースで head を phi として記述する。shapes より先に試行され、refusal 時のみ shapes に落ちる | 拡張中(退役の受け皿) |

## 3. 既知の gap(一般経路が未対応で red を許容している箇所)

現在なし。bootstrap は 2026-08-11 時点で green。

フル Kizu で書いた selfhost ソースが stage 自己コンパイルで
`compiled mir: ...` の refusal を出したら、ここに
「日付 / 関数 / refusal メッセージ / 最小 probe へのリンク」を追記し、
nightly bootstrap の red をこの表と突き合わせて退行と区別する。

## 4. 退役の進め方(PR #1492 を雛形に)

1. 対象の専用 lowering が何を前提にしているかを、生成 LLVM とソースの突き合わせで確認
2. `selfhost/tests/probes/` に挙動 probe を追加(退役後の正しさは probe と parity が持つ)
3. dispatch から外し、一般経路の named refusal を潰す
4. 専用コードと構造 pin を削除し、この台帳の Status を retired に更新
