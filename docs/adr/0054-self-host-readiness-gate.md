# ADR-0054: self-host migration readiness gate

## Status

採用

## Context

Kizu の self-host compiler は module-first で移行する方針になった。
ただし、Go 実装をそのまま Kizu ファイルへ写す前に、言語仕様と Go compiler 側の
oracle が十分に固まっている必要がある。

Kizu は Zig 的な明示性と Rust safe code 相当のメモリ安全性を両立する方針である。
そのため、移植を急いで hidden fallback、未検証の stdlib builtin、曖昧な ownership
境界を増やすことは避ける。

## Decision

self-host compiler の file/module 単位移植は、各 component ごとに readiness gate を
通してから進める。

readiness gate は次を満たす必要がある。

1. Go compiler がその component の source-of-truth oracle を持つ。
2. Kizu 側 module が Go 側 package 境界と 1:1 に対応している。
3. 使用する言語機能が `SPEC.md` または ADR に記録されている。
4. 使用する stdlib API が `std/` skeleton、`docs/stdlib.md`、conformance のいずれかで
   追跡されている。
5. memory-safety boundary が safe Kizu の範囲で静的に検査できる。
6. positive example と negative diagnostic test がある。
7. build/cache/performance への影響を測定する方法がある。

この gate を満たす前に、Go 実装を Kizu 実装へ置き換えてはいけない。

## Recommended Readiness Decisions

Kizu の思想に基づく推奨判断は次の通り。

| 項目 | 推奨判断 | 理由 |
| --- | --- | --- |
| 移植順 | token/lexer から始める | ownership が単純で、Go lexer oracle と比較しやすい |
| parser 移植 | token/lexer 完了後 | AST、Array、diagnostic、error union が絡むため |
| type/ownership 移植 | parser/resolver 完了後 | memory-safety の中核なので oracle なしで移植しない |
| module 参照 | `import selfhost::token;` + `token::Name` | namespace と field access の責務を分ける |
| cross-module 型参照 | Go compiler 側で先に完成させる | self-host lexer が `token::Token` を返せないと移植が進まない |
| error | `!T` を recoverable failure に使う | diagnostic を失わず、Zig 的に明示的 |
| optional | `?T` は absence のみ | failure と absence を混ぜない |
| collection | `std::array::Array<T>` は allocator/deinit 明示 | allocation と cleanup を隠さない |
| slice | `[]u8` を source view に使う | primitive string を復活させない |
| test | `kizu test` は self-host component test の前提 | Go test だけでは Kizu 製 compiler の開発体験を検証できない |
| legacy harness | `frontend.kizu` は freeze する | 新しい production logic を巨大単一ファイルに足さない |

## Consequences

- self-host 移植は遅く見えるが、Go/Kizu の 1:1 oracle を保ったまま進められる。
- 仕様が未確定なまま Kizu compiler code を増やすことを避けられる。
- `token` / `lexer` 以外の移植は、必要な module/type/stdlib/test 機能が固まるまで
  block できる。
- 最終判断は各 issue の受け入れ条件で行う。
