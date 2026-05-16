# ADR-0053: self-host readiness gate

## Status

採用

## Context

Kizu の self-host compiler は、Go compiler を十分な oracle として育ててから
Kizu modules へ移植する。ただし、Go compiler 全体を完成させ切るまで self-host
作業を止めると、Kizu で compiler を書くために不足している stdlib、module
boundary、diagnostic、memory-safety rule が見えにくくなる。

一方で、未固定の仕様や弱い oracle のまま Kizu へ移植すると、Go 実装と Kizu
実装の差分が hidden fallback や曖昧な TODO として残る。Kizu は memory safety、
明示的な capability、予測可能な build/cache behavior を重視するため、移植可能
かどうかを component ごとに判定する gate が必要である。

## Decision

self-host migration は component readiness gate を通ったものから進める。

Go compiler は当面 production path と oracle の両方を担当する。Kizu 実装は、
component ごとに次を満たすまで production path を置き換えない。

1. 対象 component の仕様と非目標が `SPEC.md`、ADR、または docs に明文化されている。
2. Go implementation が同じ input shape の oracle を持つ。
3. positive fixture と negative fixture が conformance manifest または component test
   に登録されている。
4. diagnostics は message substring だけでなく、必要な span / related span を比較する。
5. memory-safety に関わる component は use-after-move、borrow escape、mutable borrow
   conflict、resource cleanup boundary を Go checker と同じ結果で比較する。
6. self-host module が必要とする stdlib API、allocator、error union、optional、
   deinit、borrow boundary が明示されている。
7. build/cache に影響する場合は no-op rebuild、cache key input、cache size への影響を
   測定または受け入れ条件に含める。
8. fallback が必要な場合は Go-owned と明記し、Kizu-owned と混ぜない。

## Migration Rule

移植は次の順序で進める。

1. token / lexer
2. AST / parser
3. diagnostics / resolver
4. type checker
5. ownership / borrow checker
6. IR
7. backend smoke contract
8. cache switch contract

各 component の Pull Request は、該当 Issue に readiness checklist を持ち、PR 本文に
実行した oracle test と conformance test を記録する。

## Consequences

- Go compiler は self-host が十分になるまで production path であり続ける。
- Kizu compiler modules は `selfhost/src` の通常 package として育てる。
- `selfhost/frontend.kizu` は legacy oracle harness として残すが、新規移植の正は
  `selfhost/src` に置く。
- self-host で不足した stdlib API は local TODO ではなく GitHub Issue と docs に戻す。
- readiness gate を満たさない component は移植せず、まず Go oracle または仕様を強化する。

