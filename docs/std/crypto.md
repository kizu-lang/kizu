# std::crypto

protocol の安全性が乗る算術と、それが必要とする乱数です。message に名前を付ける
digest、書き手を証明する tag、どこで違ったかを漏らさない比較は全部 Kizu source で、
どの target でも同じ bytes を返します。C を link しない wasm module も native
binary と同じ digest を出します。乱数だけは host から来ます。

```text
std::crypto::sha256(bytes: []u8, digest: &var []u8) -> void
std::crypto::hmac_sha256(key: []u8, message: []u8, tag: &var []u8) -> void
std::crypto::hkdf_extract(salt: []u8, keying_material: []u8, key: &var []u8) -> void
std::crypto::hkdf_expand(key: []u8, info: []u8, out: &var []u8) -> void
std::crypto::equal_constant_time(a: []u8, b: []u8) -> bool

std::crypto::random_bytes(io: Io, allocator: Allocator, count: i64) -> Error!String
std::crypto::random_into(io: Io, allocator: Allocator, out: &var String, count: i64) -> Error!void

std::crypto::Error    IoFailing | OutOfMemory | ReadFailed
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

## 鍵導出

`hkdf_extract` と `hkdf_expand` は RFC 5869 の HKDF の 2 段です。`extract` は一様で
ない keying material(鍵交換の共有秘密)と salt から 32 byte の擬似乱数鍵を
`key` の先頭に書きます。salt が空なら RFC が salt 無しのときに使う salt です。
`expand` はその鍵と、用途を名指す `info` から、`out` の長さぶんの bytes を
書きます。長さは 255 block(8160 byte)までで、それより長い view は標準が出力を
定めない誤用なので trap します(block 番号を wrap させて繰り返しを出すより)。
TLS 1.3 の key schedule はこの 2 段に TLS 自身の info を与えたものです。

## 比較

`equal_constant_time` は 2 つの view が同じ bytes かを、長さだけに依存する時間で
答えます。`std::mem::equal_bytes` は最初の不一致で止まり、かかった時間が「先頭
何 byte が合っていたか」を言います。受け取った tag をそう比べると、tag は
1 byte ずつ当てられます。長さが違う 2 つは等しくなく、それは即座に決まります。
tag の長さは秘密ではありません。

## 乱数

`random_bytes` は host の乱数源から `count` byte を引き、owner の String で返します。
native では kernel の entropy(`getentropy`)、WASI では `random_get`、browser では
`crypto.getRandomValues` です。`random_into` は呼び手の String の末尾に append し、
失敗したら String は元のままです。`count` が 0 以下なら何も引きません。

引くには `Io` が要ります。乱数は host の境界で、他の host 境界と同じく呼び出しが
source に見え、`std::testing::failing_io()` は `IoFailing` で拒否します。
`std::rand` は seed から決まる列で、test が欲しいもの、鍵にしてはいけないものです
(`docs/std/rand.md`)。

## 今は話さないこと

時計はここにありません。証明書の有効期限のように「今」が要る検証は、呼び手が
`std::process::unix_millis()` を渡し、そう書いたことが source に残ります。

検証は公開されている test vector です。FIPS 180-4 の例と RFC 4231 の case が
`tests/behavior/src/crypto/` にあり、`examples/crypto_sha256.kizu` が約束する
hex は compiler の test が Go の実装と突き合わせます。
