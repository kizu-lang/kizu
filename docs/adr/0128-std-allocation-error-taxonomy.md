# ADR-0128: std の allocation 失敗は `std::mem::Error::OutOfMemory` の 1 値にする

Status: 採用

## 背景

`catch`(ADR-0127)で `E!T` は call site で処理できるようになったが、std の
allocation 失敗はどれも catch できなかった。原因は 2 つ重なっている。

1. container / string の mutator(`Array.append`、`String.append_bytes`、
   `Map.insert` など)が bare `!void` を返す。`!T` は set を持たないので
   `catch` の対象にならない(SPEC §11.1)。
2. OutOfMemory が `std::mem::Error` / `std::array::Error` / `std::map::Error`
   の 3 set に別 member として重複宣言されている。error 値は per-set identity
   なので、この 3 つは大域コードが違う別の値になる。仮に mutator へ set を
   宣言し合成で束ねても、bare 名 `OutOfMemory` が衝突して handler の arm は
   すべて元 set 修飾になる。「どの container の内部で尽きたか」は caller に
   とって意味のない情報で、それを綴らせる set 設計が間違っている。

この合流の帰結が `std::json::decode*` の bare `!T` で(JSON 不正 =
`json::Error` と allocation 失敗が別系統のまま名前を付けられない)、
buildcache の「壊れた entry を miss として扱う」(Go の `json.Unmarshal`
失敗 → miss)を selfhost に移植できない porting gap を作っていた。
また `String` 経由の OOM が `std::array::Error::OutOfMemory` と報告される
実装詳細の漏れも既に観察できていた(String は `Array<u8>` で実装されている)。

## 決定

1. **allocator 経由の allocation 失敗は `std::mem::Error::OutOfMemory` の
   1 値。** memory の出どころは capability としての `Allocator` であって
   container ではないので、失敗の出自も `std::mem` が持つ。
   `std::array::Error` は `{ OutOfBounds }` になり、member が
   OutOfMemory だけだった `std::map::Error` は set ごと消える。
2. **pure-Kizu の std API は bare `!` をやめ、宣言 set を返す。**
   allocation だけが失敗する操作(`append` / `reserve` / `insert` /
   `clone` / `string::from_bytes` / `mem::box` / `fmt::append_*` /
   `sort` / `path`)は `std::mem::Error!T`。境界検査だけが失敗する操作
   (`set` / `swap` / `truncate`)は `std::array::Error!T`。両系統が
   合流する `std::json::decode*` は合成で束ねる:
   `pub error DecodeError = Error or std::mem::Error;`。
   これで caller は `catch` / `else |err|` で std の失敗を網羅的に処理できる。
3. **C runtime の error code 表を mirror する set(`std::fs::Error` /
   `std::io::Error` / `std::process::Error`)は変えない。** そこにある
   OutOfMemory は runtime 内部の C malloc / 一時バッファの失敗であって
   `Allocator` の失敗ではない。runtime 契約は set を丸ごと mirror するのが
   原則で、code 表を跨いだ詰め替えを作らない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| module ごとの OutOfMemory を維持し、合成 + 元 set 修飾 arm で処理する | 「どの container の内部で尽きたか」は caller に意味がなく、握る側の全 arm が `array::Error::OutOfMemory` 形の修飾になる。衝突規則(ADR-0127)は同名別義の member を守る仕組みで、同義の member を 3 回宣言する言い訳ではない |
| mutator の bare `!void` を維持し、`!T` も catch できるよう言語を広げる | ADR-0127 で却下済み。「任意 set の member」型の新設が要り、網羅検査も効かない |
| `String` の mutator が `std::array::Error` を返す | 実装詳細(String が Array<u8> であること)が signature に漏れる。allocation 失敗の出自を 1 つにすればこの選択自体が消える |
| `std::map::Error` を空 set で残す | member の無い set は match も返却もできず、参照する手段が無い。将来 map 固有の失敗が出たら宣言し直せばよい(additive、原理 8) |
| fs / io / process の OutOfMemory も `std::mem::Error` に寄せる | これらの set は C runtime の error code 表の mirror で、runtime 内部の malloc 失敗は `Allocator` と無関係。code 表を跨ぐ詰め替え(変換)は「set をまたぐ変換は存在しない」(SPEC §11.2)に反する |
| `decode` の失敗を全部 `json::Error` に詰め替える(OutOfMemory member を json に足す) | mem の失敗を json の値に写す変換が要る。合成(`= Error or std::mem::Error`)なら変換なしで両系統をそのまま運べる。ADR-0127 の「自前 member も足したい場合は set を宣言して和に入れる」の形そのもの |
