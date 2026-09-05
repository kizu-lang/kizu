# std::date

proleptic Gregorian の暦日と、UTC の時刻です。

```text
std::date::ymd(year: i64, month: i64, day: i64) -> Error!Date   暦に無い日は InvalidDate
std::date::from_days(days: i64) -> Date                          1970-01-01 からの日数から
std::date::parse(bytes: []u8) -> Error!Date                      YYYY-MM-DD
std::date::is_leap_year(year: i64) -> bool
Date.year / month / day(self) -> i64
Date.to_days(self) -> i64                                         1970-01-01 からの日数
Date.weekday(self) -> Weekday
Date.is_leap_year(self) -> bool
Date.days_in_month(self) -> i64
Date.add_days(self, count: i64) -> Date
Date.add_months(self, count: i64, overflow: Overflow) -> Date
Date.add_years(self, count: i64, overflow: Overflow) -> Date
Date.days_since(self, earlier: Date) -> i64                       self - earlier。負にもなる
Date.before / after / equals(self, other: Date) -> bool
Date.midnight(self) -> DateTime                                   00:00:00.000
Date.at(self, hour, minute, second, milli: i64) -> Error!DateTime  範囲外は InvalidDate
Date.append_iso(self, allocator, out: &var String) -> mem::Error!void   YYYY-MM-DD

std::date::from_unix(moment: std::time::UnixTime) -> DateTime
std::date::parse_datetime(bytes: []u8) -> Error!DateTime         RFC 3339。offset は UTC に畳む
DateTime.date(self) -> Date
DateTime.hour / minute / second / milli(self) -> i64
DateTime.to_unix(self) -> std::time::UnixTime
DateTime.before / after / equals(self, other: DateTime) -> bool
DateTime.append_iso(self, allocator, out: &var String) -> mem::Error!void   YYYY-MM-DDTHH:MM:SS[.fff]Z

Weekday: Monday .. Sunday
Weekday.iso_number(self) -> i64                                    Monday 1 .. Sunday 7
Overflow: Clamp | Carry                                            add_months が月末を越えたとき
```

```kizu
let issued = try date::ymd(2024, 1, 31);
let due = issued.add_months(1, date::Overflow::Clamp);   // 2024-02-29
let today = date::from_unix(time::unix(process::unix_millis()));
let today_day = today.date();
if today_day.after(due) { ... }
```

## 作った日は暦にある

`Date` は `ymd` か `from_days` か `parse` からしか作れず、field は method で読みます。
`ymd(2023, 2, 29)` は 3 月 1 日に丸めず `Error::InvalidDate` を返すので、`Date` を
受け取った側は月と日の範囲を検査し直しません。`DateTime` も同じで、`Date.at` は
時・分・秒・ミリ秒の範囲を検査します。うるう秒は表せません(秒は 0..59)。

4 つの型はどれも copy aggregate(SPEC §8)で、代入と値渡しで複製され、cleanup を
持ちません。

## 月の算術は丸め方を書く

日の算術に曖昧さはなく、`add_days` と `days_since` は `to_days` の差です。月の算術は
1 月 31 日の 1 か月後が何日かを決めなければならないので、`add_months` /
`add_years` は `Overflow` を引数に取ります。

| | 2024-01-31 + 1 か月 | 2024-02-29 + 1 年 |
| --- | --- | --- |
| `Overflow::Clamp` | 2024-02-29(月末で止める) | 2025-02-28 |
| `Overflow::Carry` | 2024-03-02(はみ出た日を翌月へ繰り越す。Go の `AddDate`) | 2025-03-01 |

## UTC だけ

`DateTime` は UTC の読みで、timezone も locale も持ちません。`from_unix` /
`to_unix` は `std::time::UnixTime` との相互変換で、瞬間の型はそちらが持ちます。
`parse_datetime` は `+09:00` のような offset を受け取りますが、返す値は UTC に
畳んだものです。ある地域の壁時計の日付が要る場合は、offset を自分で足してから
`date()` を読みます。

`now()` はありません。今日は `date::from_unix(time::unix(process::unix_millis()))`
と書き、`Date` を取る関数は時計に触れません。

## 綴りは ISO 8601

`Date.append_iso` は `YYYY-MM-DD`、`DateTime.append_iso` は RFC 3339 の
`YYYY-MM-DDTHH:MM:SS[.fff]Z` を書きます。ミリ秒が 0 なら小数部を省き、そうでなければ
末尾の 0 を落とします(`.5`、`.05`、`.123`)。Go の `time.Time` が JSON に書く
RFC3339Nano と同じ綴りなので、両方の側が互いの stamp を読めます。

`parse` は `YYYY-MM-DD` だけを受け取り、`parse_datetime` は秒までを必須、小数部は
任意(ミリ秒より下は切り捨て)、末尾は `Z` か `±HH:MM` を必須にします。綴りが合わない
ものは `InvalidFormat`、綴りは合うが暦や時刻の範囲を外れるものは `InvalidDate` です。
0 から 9999 の外の年は ISO 8601 の拡張表記で、符号と 4 桁以上の数字
(`+10000-01-01`、`-0001-12-31`)を書き、読みます。符号の無い年は 4 桁だけです。
