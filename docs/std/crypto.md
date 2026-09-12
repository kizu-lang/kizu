# std::crypto

protocol の安全性が乗る算術です。message に名前を付ける digest、書き手を証明する
tag、どこで違ったかを漏らさない比較。全部 Kizu source で、どの target でも
同じ bytes を返します。C を link しない wasm module も native binary と同じ
digest を出します。

```text
std::crypto::sha256(bytes: []u8, digest: &var []u8) -> void
std::crypto::hmac_sha256(key: []u8, message: []u8, tag: &var []u8) -> void
std::crypto::equal_constant_time(a: []u8, b: []u8) -> bool
```

```kizu
var digest = [32]u8{};
let view = digest.as_mut_bytes();
crypto::sha256(message, view);
let bytes = digest.as_bytes();

var tag = [32]u8{};
let tag_view = tag.as_mut_bytes();
crypto::hmac_sha256(key, message, tag_view);
let mine = tag.as_bytes();
if crypto::equal_constant_time(mine, theirs) {
    // the message was written by a holder of the key
}
```

## digest と tag

`sha256` は FIPS 180-4 の SHA-256 で、`bytes` 全体の 32 byte の digest を
`digest` の先頭に書きます。`hmac_sha256` は RFC 2104 の HMAC で、`key` の下での
`message` の 32 byte の tag を `tag` の先頭に書きます。key はどんな長さでもよく、
1 block(64 byte)より長い key は先に hash され、短い key は 0 で埋められます。

結果は呼び手の buffer の view に書きます。`var out = [32]u8{}` と
`as_mut_bytes()` がその形で、hash は何も確保せず、失敗もしません。32 byte より
短い view は bounds trap です(index が末尾を越えたときと同じ)。

message は 1 回の呼び出しで全部渡します。少しずつ渡す hasher はありません。
途中の block を struct に持たせる必要があり、stack buffer は struct field に
置けないからです(SPEC §7)。組み立て中のものを hash するなら、組み立ててから
渡します。

## 比較

`equal_constant_time` は 2 つの view が同じ bytes かを、長さだけに依存する時間で
答えます。`std::mem::equal_bytes` は最初の不一致で止まり、かかった時間が「先頭
何 byte が合っていたか」を言います。受け取った tag をそう比べると、tag は
1 byte ずつ当てられます。長さが違う 2 つは等しくなく、それは即座に決まります。
tag の長さは秘密ではありません。

## 今は話さないこと

乱数と時計はここにありません。primitive が必要とするものは呼び手が渡し、
どこから来たかは呼び手の source に残ります。`std::rand` は seed から決まる列で、
暗号用途の乱数ではありません(`docs/std/rand.md`)。

検証は公開されている test vector です。FIPS 180-4 の例と RFC 4231 の case が
`tests/behavior/src/crypto/` にあり、`examples/crypto_sha256.kizu` が約束する
hex は compiler の test が Go の実装と突き合わせます。
