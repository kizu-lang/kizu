# ADR-0121: `?T` field は document が省略できる

Status: 採用

Issue: なし(#1651 が残した `?T` の穴)

## 背景

`encode<T>` は `?T` field を書ける。`decode<T>` は読めなかった。

```
$ kizu check probe.kizu
error: type error: optional `?i64` cannot be a static argument yet
```

`decode_field` が `decode_value<std::meta::field_type<T, f>>` と書いていて、
`field_type<User, nick>` = `?i64` が static argument に来るためである。
ADR-0101 が static argument への optional を閉じている。

これは迂回できる。`encode_fields` が既に同じ制約を越えている —— optional を
runtime で開き、static argument には `element<?i64>` = `i64` しか渡さない。
decode 側も `null` token を先に受けて、残りを `element<?T>` として読めば同じ
形になる。実装は決まる。

決まらなかったのは **key が document に無いときの意味**である。

```
{"name":"alice","nick":7}      → 7
{"name":"alice","nick":null}   → null
{"name":"alice"}               → ?
```

原理 8(迷ったら閉じたまま出す)の既定は `MissingField` である。閉→開は
additive で、開→閉は breaking だからだ。

## 決定

1. **`?T` field は document が省略できる。**key が無い場合も、key があって
   値が `null` の場合も `null` になる。
2. **他の field は `MissingField` のまま。**省略できるのは `?T` field だけで、
   optional は「この key は無くてもよい」を言う唯一の綴りになる。
3. **encode は省略しない。**`?T` field は必ず key を書き、値が無ければ `null`
   を書く。変更はない。

## 原理 8 の既定に従わない理由

原理 8 は「**迷ったら**閉じたまま出す」であり、tiebreaker である。ここでは
迷っていない。

**実需がはっきりしている。**null field を省略する producer は例外ではなく
普通である。Zig の `std.json` は encode 側に `emit_null_optional_fields` を
持ち、Go の `encoding/json` は `omitempty` を持つ。閉じた版は、一番よくある
document を読めないという結果になる。

**`?T` はその差を持てない。**「key が無い」と「key があって null」を分けても、
`?i64` はどちらも「値が無い」に潰れる。原理 7 の「意味が違うなら畳まない」は
差が型に現れるときの規則で、ここでは現れない。表現できない区別を error と成功
の差として利用者に課すのは、原理 6(型を増やして区別を課さない)に当たる。

**`MissingField` が固有に守っていた事故は、ほとんど残っていない。**「key が
消えたのは producer が rename したから」というのが危険な側だが、それは
`decode` の未知 key 拒否が既に捕まえる。

```
$ kizu run unknown.kizu    # {"name": "alice", "nickname": 7}
runtime error: std::json::Error::UnknownField
```

省略として通るのは、対応する未知の key が 1 つも無い document だけである。

## 却下した代替案

| 案 | 却下理由 |
| --- | --- |
| **key 必須(`MissingField`)** | 一番よくある document を読めない。緩めるのが additive なのは事実だが、実需が既に見えている以上、先送りが買うものが無い |
| **field default(Zig 方式)** | Zig は `nick: ?i64 = null` で省略を opt-in する。struct field の default 値は言語機能で、struct literal が全 field を一度に取る規則と owner field の所有規則に触る。json 1 機能のために言語を変えない |
| **encode に「null を省略」option** | 読める document は 1 つも増えない。逆に Kizu が「自分で書いたのに自分で読めない document」を作れるようになり、decode 側の緩和を強制する(原理 9)。option record を持たない今の API 形にも合わない |
| **3 つ目の decode 入口** | `decode_missing_as_null` のような名前は原理 3(明示語)に見えるが、`decode` / `decode_ignore_unknown` と掛け算になる(原理 10) |
| **`decode_value<?T>` を書けるようにする** | ADR-0101 を開く判断になる。`encode_fields` が既に迂回しているので、std 側に必要が無い |

## 再評価条件

- 「この key は必ず来る、ただし値は null かも」と言う実需が現れたとき。開→閉は
  breaking なので、`?T` の意味を変えるのではなく別の綴り(field default など)
  を足す判断になる。
- encode 側に null 省略が必要になったとき。そのとき `std::json` が option
  record を持つかを問い直す(`docs/std/json.md` が knob 3 つ目で問い直すと
  書いている)。
- ADR-0101 が static argument への optional を開いたとき。`decode_optional` の
  迂回は不要になるが、省略の意味はこの ADR のまま。
