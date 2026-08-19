# ADR-0112: std::json encode v1 は出力を所有し、API 誤用は trap にする

Status: 採用

Issue: #1086(encode の error contract)、#1078(reflection encode の前提)

## 背景

`std::json` の encode v1 を、hidden hook / derive / method discovery のない
明示 streaming writer として作る。決めるべきは 2 つだった。

1. begin/end の対応や「object の外で field を書く」といった **API 誤用を
   どう扱うか**(型で閉じるか、error 値にするか、trap にするか)
2. 公開 API の戻り値を untyped `!void` にするか `std::json::Error!void` に
   するか

原理 5(型で閉じられる検査は compile 時に閉じる)から、まず typestate で
閉じられないかを検討した。

- **move typestate**(`begin_object` が encoder を move で飲み、`end_object`
  が親を返す)は、end 忘れまで deinit 義務違反として compile error にできる。
  しかし writer の型が入れ子の深さごとに変わるため、深さが実行時データで
  決まる再帰的データ(自己参照 struct、JSON 自身の DOM)を `encode<T>` で
  扱えない。instantiation が停止しない。
- **borrow typestate**(writer が `&var Encoder` を field に持つ)は SPEC §9
  の「borrow field は struct / union に保存できない」を開くことになる。
  ADR-0100 が view 捕捉ですら必要とした soundness 作業を排他 borrow に広げ、
  解析を関数横断にする(原理 11)。しかも borrow だけの struct には deinit
  義務が無いので、**end 忘れは閉じられない** —— 最大のコストで最小の効果。

再帰的データの encode は必要と判断した(JSON は入れ子 format であり、
自己参照 struct も実需がある)ため、move typestate は落ちる。borrow
typestate は値段に見合わない。

## 決定

### 形

Encoder は**出力 String を所有**する。borrow を field に持たないので、
今の言語規則のまま書ける。

```kizu
var encoder = json::encoder(allocator);
defer encoder.deinit();

try encoder.begin_object();
try encoder.write_i64_field("id", 7);
try encoder.begin_object_field("profile");
try encoder.write_bytes_field("city", "kyoto");
try encoder.end_object();
try encoder.end_object();

try encoder.finish_into(out);
```

### 誤用は trap、error 値ではない

`Error::InvalidState` を持たない。「object の外で field を書いた」
「end が対応していない」「top-level 値が 2 つ目」「finish 時に container が
開いている」は回復可能な実行時条件ではなく**プログラムの bug** なので、
`std::internal::builtin::panic(message)` で止める。原理 7: 回復する失敗と
bug を同じ channel に相乗りさせない。

その結果 **encode 側の error set は空**になる。`!void` は所有する String の
allocation 失敗を伝播するためだけにある。

### 誤用を設計で消す

trap に落とすのは「型で閉じられなかったもの」だけで、まず API の形で消す。

- `write_*_field(name, value)` が key と value を 1 呼び出しに束ねる。
  key だけ書いて value を忘れる形が**書けない**
- 出力は `finish_into` からしか取れない。閉じ忘れたまま出力を使うことは
  できず、必ず検出される

### 検査領域

入れ子状態は「深さ × (object か array か × 要素を書いたか)」で、i64 2 本の
bit stack に持つ(push は `*2`、pop は `/2`)。確保は起きず、62 段を超える
入れ子は trap。この状態は**誤用検出のためだけ**にあり、出力の正しさを
駆動しない。将来の `encode<T>` は型の構造に沿って再帰するので出力は構造上
well-formed であり、検査なしの内部経路を使える。

### finish は view ではなく copy

`finish_into(out: &var String)` が完成した document を caller の String へ
append する。`fn (self: Encoder) finish() -> []u8` と書けるのが自然だが、
今の checker は method の field(`self.out`)の bytes view を関数の外へ
貸せない(ADR-0100 は let 初期化位置の view 捕捉までで、method 戻り値の
provenance は対象外)。view を返す形は provenance 拡張が入ってから。

### escape

byte 列は決定的に escape する(`"`、`\`、newline、CR、tab は 2 文字 escape、
その他の control byte は `\u00XX`)。encode v1 は UTF-8 validation をしない
ので、任意の byte 列で escape が失敗することはない。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| move typestate(`begin` が move、`end` が親を返す) | 深さごとに writer 型が変わり、再帰的データの `encode<T>` が instantiation で停止しない |
| borrow typestate(`&var Encoder` を field に持つ) | SPEC §9 の borrow field 禁止を開き解析を関数横断にする(原理 11)。しかも end 忘れを閉じられない |
| `Error { Message([]u8), InvalidEncoderState, ... }`(#1086 の初案) | error は payload を持たない(ADR-0086)。`E!T` の `try` は同じ set しか伝播できないので、内部の `array::Error::OutOfMemory` を通せない |
| 誤用を回復可能な `Error::InvalidState` にする | bug と実行時失敗が同じ error に相乗りする(原理 7)。caller に握って回復する意味がない |
| 状態を allocator 付き storage に持つ | encode 状態は誤用検出専用で、深さは手書き source の形に従い浅い。allocator を 2 つ並べる構築(原理 4 の明示)に見合わない |

## 影響

- `lib/kizu/std/src/json.kizu` 追加(`Encoder` と streaming API)
- `std::internal::builtin::panic(message: []u8) -> void` を追加。既存の
  `test_fail` と同じ経路(IR `panic.fail` → `kizu_panic_fail`)で、
  `runtime error: <message>` を stderr に出して abort する。std が「これは
  bug」と言える唯一の口で、safe user code からは名前を綴れない
- `String.append_string(other: &String) -> !void` を追加(所有 buffer 同士の
  copy。`finish_into` が使う)
- ownership checker: method 引数の view lend を関数引数と揃えた。
  `checkUserCallArg` は ADR-0100 rule 3 の条件(戻り値も `&var` parameter も
  view を運べない)で lend を許すのに、`checkImplMethodArg` にその分岐が無く、
  `encoder.write_bytes_field(name, string.as_bytes())` が書けなかった。
  receiver も判定対象の parameter に含むので、view field を持つ `&var self`
  への lend は従来どおり拒否する
- SPEC §14 に `std::json` の encode v1 契約と `append_string` を追記
- wasm backend は `union.tag` 未対応のため std::fmt を使う既存 example と
  同じく json も native 経路のみ(この ADR が変える点ではない)

## 再評価条件

- method 戻り値の borrow provenance が入ったとき、`finish_into` を
  `finish() -> []u8` に戻す
- `encode<T>` を入れるとき、reflection 側は検査なしの内部経路を使う
  (#1078)。対象は所有の木(値・struct・`Box`・`Array`・`?T`・`Map`)に
  限り、`Handle<T>` は共有参照で JSON の木と対応しないため compile error
