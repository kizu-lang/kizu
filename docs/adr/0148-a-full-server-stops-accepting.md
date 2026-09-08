# ADR-0148: 抱える接続の数は accept を止めて守る

## Status

Accepted.

## Context

`first` / `next` の server は接続を無制限に抱えられる(#1083)。1 本の費用は
`first` / `next` では buffer だけ、TaskSet の worker では約 269 KiB(ADR-0139)で、
`max_requests` は 1 本の仕事を有限にするだけで本数を抑えない。

決めることは 3 つ: 何を数えるか、上限で何をするか、どこに置くか。

## Decision

Go の `netutil.LimitListener` に寄せる。

- **数えるのは accept して閉じていない接続。** 手渡している 1 本も含む。in-flight
  request は数えない —— HTTP/1 は接続ごとに 1 つしか処理中にならないので同じ数に
  なる。
- **上限では accept を止める。** caller は kernel の backlog で待ち、接続が 1 本
  閉じれば次が accept される。断りも drop もしない。
- **`Limits.max_connections`** に置く。既定は 0(上限なし)で Go と同じ。測らずに
  置ける数がない。
- 上限の間、listener は poller から外す。入れたままでは accept しない caller の
  ために wait が毎回起きる。

TaskSet の worker loop は接続を server の外へ持ち出すので、server は close を
見ない。そちらは loop 自身が同じ規則を書く。TaskSet が `running()`(終わっていない
worker の数)と `wait_one()`(1 つ抜けるまで loop を回す)を持ち、loop は上限で
`wait_one` を呼ぶ。数えるのは worker で、1 worker が 1 接続を持つので同じ数になる。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 上限で accept して 503 を返す | 減らしたい相手に accept と write を払う。Go にも無い。backlog が溢れれば kernel が落とす方が安い |
| in-flight request の上限を別に持つ | HTTP/1 では接続数と同じ数。pipelining も 1 つずつ答える(`docs/std/http.md`)ので別の数にならない |
| 有限の既定(1024 など) | 1 接続の費用が loop の形で 100 倍違い、1 つの数が両方に合わない。`max_requests` の 100 は測って置いた数(ADR-0139) |
| 上限の間も listener を poller に残す | accept しない caller のために wait が即時に返り続ける busy loop になる |
| 黙って接続を落とす | 隠れた drop policy(原理 2) |
| TaskSet に上限を持たせ `spawn` の中で待つ | 待ちが `spawn` に隠れる。loop は caller のもの(ADR-0144)で、待つ場所も caller が書く |
| 上限で `spawn` が失敗する | 断る policy。失敗を受けた loop は結局待つか落とすかを書くことになる |
