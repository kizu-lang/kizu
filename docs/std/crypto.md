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
std::crypto::x25519(scalar: []u8, point: []u8, out: &var []u8) -> Error!void
std::crypto::x25519_base(scalar: []u8, out: &var []u8) -> void
std::crypto::ecdsa_p256_verify(public_key: []u8, digest: []u8, signature: []u8) -> bool
std::crypto::equal_constant_time(a: []u8, b: []u8) -> bool

std::crypto::chacha20_poly1305_seal(allocator, key: []u8, nonce: []u8, aad: []u8, plain: []u8, out: &var String) -> std::mem::Error!void
std::crypto::chacha20_poly1305_open(allocator, key: []u8, nonce: []u8, aad: []u8, sealed: []u8, out: &var String) -> Failure!void
std::crypto::aes_gcm_seal(allocator, key: []u8, nonce: []u8, aad: []u8, plain: []u8, out: &var String) -> std::mem::Error!void
std::crypto::aes_gcm_open(allocator, key: []u8, nonce: []u8, aad: []u8, sealed: []u8, out: &var String) -> Failure!void

std::crypto::random_bytes(io: Io, allocator: Allocator, count: i64) -> Error!String
std::crypto::random_into(io: Io, allocator: Allocator, out: &var String, count: i64) -> Error!void

std::crypto::Error    IoFailing | OutOfMemory | ReadFailed | AuthenticationFailed | LowOrderPoint
std::crypto::Failure  Error or std::mem::Error
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

## 鍵合意

`x25519` は RFC 7748 の X25519 で、`scalar`(自分の秘密鍵、32 byte)を `point`
(相手の公開鍵、32 byte)に掛けた 32 byte を `out` の先頭に書きます。`x25519_base`
は秘密鍵から公開鍵を作ります(base point u = 9 に掛けたもの)。双方がそれぞれ自分の
秘密鍵を相手の公開鍵に掛けると同じ 32 byte になり、公開鍵しか見ていない者には
分かりません。秘密鍵は `random_bytes` で引いた 32 byte そのままでよく、RFC の言う
clamp は中でします。

相手の point が低位数(u = 0、1、p - 1 など)なら結果は全 0 で、それは相手が共有
秘密を誰にでも分かるものにしたということなので、`LowOrderPoint` で拒否します
(RFC 7748 §6.1、TLS 1.3 は RFC 8446 §7.4.2 でこの検査を求めます)。長さが 32 でない
scalar や point は誤用で trap します。

体の算術は 16 bit の limb 16 本を `i64` で持つ TweetNaCl の形で、ladder の swap は
mask で branch ではなく、時間は秘密鍵に依らないです。1 回の乗算は `--opt` で
1 ms 弱です(Go の assembly は 0.05 ms)。

## 署名

`ecdsa_p256_verify` は P-256(secp256r1)上の ECDSA 署名を検証します(FIPS 186-4
§6.4.2)。`public_key` は SEC 1 の非圧縮 point 65 byte(`04`、x、y)で、証明書の
SubjectPublicKeyInfo が持つ形そのものです。`signature` は r と s を 32 byte big-endian
で並べた 64 byte、`digest` は SHA-256 の 32 byte です。鍵や署名が別の形、鍵が曲線上に
ない、r や s が 1..n-1 の外、のどれも「不正な署名」であって bug ではないので false を
返します。digest が 32 byte でないのは呼び手の誤用で trap します。

検証しか無いのは、署名には秘密鍵と 2 度使ってはならない nonce が要り、client には
どちらもまだ無いからです。DER の `SEQUENCE { INTEGER r, INTEGER s }` から r と s を
取り出すのは X.509 側の仕事です。

算術は 32 bit limb 8 本を `u64` で持つ Montgomery 乗算で、体 p と位数 n の両方に同じ
code を使います。point は Jacobian 座標で、u1 G + u2 Q は Straus–Shamir の同時
double-and-add です。扱うのは公開鍵と署名だけなので、値で branch し早く比べます。
1 回の検証は `--opt` で約 1 ms です。

## 封をする

`chacha20_poly1305_seal` は RFC 8439 の AEAD_CHACHA20_POLY1305 で、`plain` の
暗号文と 16 byte の tag を `out` の末尾に append します。`aad` は認証されるが暗号化
されない bytes で、相手が開く前に読む record header がそれです。
`chacha20_poly1305_open` は `sealed`(暗号文の後ろに tag)の tag を先に定数時間で
検査し、通ったものだけ復号して `out` に append します。合わなければ何も append
せず `Error::AuthenticationFailed` です。message が変わったのか、この key / nonce /
aad で封をしたものでないのかは分からず、言いません。

`aes_gcm_seal` / `aes_gcm_open` は NIST SP 800-38D の AES-GCM で、同じ形です。
key が 16 byte なら AES-128、32 byte なら AES-256。AES は table を引かず、S-box を
bitslice した word 上の論理回路(Boyar–Peralta)で計算し、GHASH の GF(2^128) 乗算は
bit を 4 つおきに広げた整数乗算で書いてあります(BearSSL の `aes_ct64` と
`ghash_ctmul64` の移植、MIT)。どちらも key や text が address や分岐を選ばないので、
かかる時間が key を言いません。table 引きの AES は cache timing で key が漏れます。

ChaCha20-Poly1305 の key は 32 byte、nonce は 12 byte で、違う長さは誤用なので
trap します(AES-GCM も同様)。nonce は 1 つの key の下で 1 回だけ使います。同じ組で
2 つの message を封じると、2 つの平文の XOR が漏れ、GCM では認証鍵も漏れます。
どの nonce を使うかは protocol の決めることで(TLS は record を数える)、ここでは
引きません。

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

検証は公開されている test vector です。FIPS 180-4 の例、RFC 4231、RFC 5869、
RFC 8439、RFC 7748、RFC 6979 の case が `tests/behavior/src/crypto/` にあり、`examples/`
の crypto example が約束する hex は compiler の test が Go の実装と突き合わせます。
