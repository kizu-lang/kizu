# ADR-0032: v0.1 はメモリ安全性監査を release gate にする

Status: 採用

## 背景

Kizu の中核価値は、safe Kizu のメモリ安全性である。
v0.1 は Go 製 interpreter による language core release だが、interpreter-first であっても
ownership、move、borrow、arena / handle、`@unsafe` 境界の仕様は曖昧にできない。

機能が動くだけでは v0.1 完了とはしない。
safe Kizu のメモリ安全性を、仕様、実装、負例 test、examples、issue の受け入れ条件で
確認できる状態を v0.1 の release gate にする。

## 決定

Kizu v0.1 は、safe Kizu に対して次を release blocker として扱う。

- use-after-move を許さない
- double move を許さない
- borrow 中の値の move を許さない
- borrow escape を許さない
- borrow を struct field に保存させない
- borrow を task / comptime / `@unsafe` 境界で延命させない
- `std::arena::Arena<T>.get(std::arena::Handle<T>)` は local borrow だけを返す
- handle は対応する arena 以外に使えない
- handle は raw pointer として扱えない
- mutable borrow conflict を検査する
- `@unsafe` は type check / move check / borrow check を全面的に無効化しない
- raw pointer operation、C ABI call、unchecked operation は safe Kizu の保証外として明示する

v0.1 release 前に、次を必ず満たす。

- 上記 blocker ごとに positive / negative example または checker test がある
- `examples/negative/` のメモリ安全違反は `kizu check` で失敗する
- safe example は `kizu check` と `kizu run` の対象として維持する
- `@unsafe` example は capability 境界と programmer obligation を読める形で示す
- SPEC の safe / `@unsafe` 境界と実装の診断が矛盾しない
- 未実装の低レベル機能を、safe Kizu の保証対象として表現しない

## 影響

- v0.1 の release issue にはメモリ安全監査を必須 checklist として含める
- checker を拡張するときは、対応する negative example を同じ変更に含める
- `@unsafe` を拡張するときは、safe check が無効化されていないことを test で確認する
- allocator、raw pointer runtime operation は、実装するまで v0.1 の安全保証に含めない
