# std::time

ミリ秒で数える瞬間と幅です。型が時計を区別します。

```text
std::time::millis(count: i64) -> Duration
std::time::seconds(count: i64) -> Duration
std::time::minutes(count: i64) -> Duration
std::time::hours(count: i64) -> Duration
std::time::days(count: i64) -> Duration              24 時間。暦の 1 日ではない
Duration.as_millis(self) -> i64
Duration.as_seconds / as_minutes / as_hours / as_days(self) -> i64   0 に向けて切り捨て
Duration.add(self, other: Duration) -> Duration
Duration.sub(self, other: Duration) -> Duration
Duration.times(self, count: i64) -> Duration
Duration.less_than(self, other: Duration) -> bool
Duration.greater_than(self, other: Duration) -> bool
Duration.equals(self, other: Duration) -> bool

std::time::instant(millis: i64) -> Instant           process::monotonic_millis() の読みから
Instant.as_millis(self) -> i64
Instant.add(self, span: Duration) -> Instant
Instant.sub(self, span: Duration) -> Instant
Instant.since(self, earlier: Instant) -> Duration    self - earlier。負にもなる
Instant.before / after / equals(self, other: Instant) -> bool

std::time::unix(millis: i64) -> UnixTime             process::unix_millis() の読みから
std::time::unix_seconds(count: i64) -> UnixTime      epoch 秒から(JSON、token の expiry)
UnixTime.as_millis(self) -> i64
UnixTime.as_seconds(self) -> i64                     0 に向けて切り捨て
UnixTime.add / sub(self, span: Duration) -> UnixTime
UnixTime.since(self, earlier: UnixTime) -> Duration
UnixTime.before / after / equals(self, other: UnixTime) -> bool
```

```kizu
let started = time::instant(process::monotonic_millis());
let outcome = work(io, allocator);
let elapsed = time::instant(process::monotonic_millis());
let took = elapsed.since(started);
if took.greater_than(time::seconds(5)) { ... }
```

## 3 つの型

`Duration` は幅、`Instant` は monotonic clock 上の瞬間、`UnixTime` は Unix epoch から
数えた壁時計上の瞬間です。どれも `i64` のミリ秒 1 つを持つ copy aggregate(SPEC §8)で、
代入と値渡しで複製され、cleanup を持ちません。

2 つの時計は互いに合意しないので、瞬間の型は method を共有しません。`Instant.since` は
`Instant` を、`UnixTime.since` は `UnixTime` を取り、混ぜると数が狂うのではなく type
error になります(`examples/negative/std_time_mixed_clocks.kizu`)。

## 時計を読まない

この module に `now()` はありません。瞬間は `std::process` の読み 1 つから作り、
`time::instant(process::monotonic_millis())` と source に書きます。`Instant` を取る
関数は時計に触れないので、test は好きな瞬間を渡せます。

`Instant.as_millis` は作ったときの読みをそのまま返します。monotonic なミリ秒の
deadline を取る setter(`std::net`)へ渡す形です。

## 暦を持たない

`days(1)` は 24 時間で、`UnixTime` は数であって日付ではありません。年・月・日、
曜日、ISO 8601 は [`std::date`](date.md) の仕事です。

## 算術

`+ - *` は `i64` の規則で wrap します(SPEC §6.9.2)。単位の変換は割り算なので 0 に
向けて切り捨て、`millis(-1999).as_seconds()` は `-1` です。比較は `less_than` /
`greater_than` の両向きがあり、receiver は binding でなければならない(SPEC §6.5)ので、
比べたい値を receiver に、閾値を引数に置くと 1 行で書けます。`since` は引数の方が後の
瞬間なら負の `Duration` を返します。
