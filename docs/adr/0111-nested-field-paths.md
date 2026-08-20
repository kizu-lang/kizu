# ADR-0111: field path の borrow / method receiver を N 段に広げる

Status: 採用

Issue: #1617

## 背景

borrow 引数と method receiver の field path は 1 段の direct field に
限定されていた(ADR-0067)。`&owner.field` と `owner.field.method()` は
通るが、`&s.current.budget` や `o.log.lines.append(7)` は
`only supports one direct field` で拒否される。

この上限は SPEC §9 に明記されていたが、意図としては恒久設計ではなく
「borrow / partial-cleanup model が必要とするまで延期」だった
(ADR-0067 Consequences)。#1617 が示したとおり、上限に当たった利用者は
1 段ずつ降りるだけの wrapper method を型ごとに量産するか、struct を
平らに潰して型の単位を失うかを迫られる。どちらも原理 10
(定型が量産される設計は間違っている)と衝突する。

実行時コストは存在しない。nested path の borrow も 1 段と同じく
pointer(GEP の連鎖)に落ちるだけで、判定はすべて compile 時に閉じる。

## 決定

borrow 引数・borrow let・method receiver・capture receiver の field path を
local binding を root とする任意の深さに広げる。

- 借用の追跡は root binding 上で dotted path(`"current.budget"`)を key に
  行う。従来の field 名 1 個は path の 1 段の場合として同じ表現に含まれる。
- 衝突判定は「一致」から「path の重なり」になる。重なりは一方が他方を
  segment 境界で含むこと: `a.b` は `a.b.c` および `a.b` と重なり、
  `a.c` や `a.bc` とは重ならない。
  - 重なる path の borrow / assignment / move は従来の同一 field と同じく拒否
  - disjoint な path への assignment は従来の disjoint field と同じく許可
- `&var` receiver の two-phase 予約(ADR-0106)は path をそのまま運ぶ。
  引数が receiver と重なる path を borrow すれば従来どおり衝突する。
- destructive cleanup(`deinit`)だけは direct field 1 段のまま残す。
  `self.a.b.deinit()` は中間型 `a` 自身の cleanup を迂回するため拒否し、
  `a` を先に取り出すことを求める(ADR-0067 の境界を維持)。各型は自分の
  field を自分の cleanup で閉じる。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 1 段上限を維持し SPEC に理由を明記するだけ | wrapper method の量産と struct の平坦化圧が残る(原理 10) |
| cleanup も N 段に広げる | 中間型の cleanup を迂回する。型ごとの deinit という既存の境界を壊す |
| path を型で表現し borrow checker を place ベースに全面改修 | 得る精度が同じで改修範囲だけ大きい。dotted path key + 重なり判定で同じ安全性が出る |

## 影響

- ownership checker: borrow 追跡 map の key が dotted path になり、衝突判定が
  重なり判定になる(`fieldPathsOverlap`)。receiver 投影は重なる path の
  borrow 数を合算する
- types checker: `&var` place・receiver path・capture receiver の形状判定が
  path の root 解決になる(`ast.FieldPathRoot`)
- IR: `field.addr` projection を hop ごとに連鎖(`lowerFieldStorage`)。
  root local の slot 化は path の root まで遡って判定(`markIfName`)
- SPEC §6.5 / §9 / §10 更新、examples / behavior test 追加
