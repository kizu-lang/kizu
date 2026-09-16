# std::crypto

暗号 protocol に必要な算術(hash、MAC、鍵導出、鍵合意、署名検証、AEAD、定数時間
比較)と乱数です。乱数以外は全部 Kizu source で、どの target でも同じ bytes を
返します。C を link しない wasm module も native binary と同じ digest を出します。
乱数だけは host から取得します。

```text
std::crypto::sha256(bytes: []u8, digest: &var []u8) -> void
std::crypto::sha512(bytes: []u8, digest: &var []u8) -> void
std::crypto::sha384(bytes: []u8, digest: &var []u8) -> void
std::crypto::hmac_sha256(key: []u8, message: []u8, tag: &var []u8) -> void
std::crypto::hkdf_extract(salt: []u8, keying_material: []u8, key: &var []u8) -> void
std::crypto::hkdf_expand(key: []u8, info: []u8, out: &var []u8) -> void
std::crypto::x25519(scalar: []u8, point: []u8, out: &var []u8) -> Error!void
std::crypto::x25519_base(scalar: []u8, out: &var []u8) -> void
std::crypto::ecdsa_p256_verify(public_key: []u8, digest: []u8, signature: []u8) -> bool
std::crypto::ecdsa_p384_verify(public_key: []u8, digest: []u8, signature: []u8) -> bool
std::crypto::rsa_pkcs1_verify(hash: Hash, modulus: []u8, exponent: []u8, digest: []u8, signature: []u8) -> bool
std::crypto::rsa_pss_verify(hash: Hash, modulus: []u8, exponent: []u8, digest: []u8, signature: []u8) -> bool
std::crypto::digest_length(hash: Hash) -> i64
std::crypto::digest_of(hash: Hash, bytes: []u8, digest: &var []u8) -> void
std::crypto::equal_constant_time(a: []u8, b: []u8) -> bool

std::crypto::chacha20_poly1305_seal(allocator, key: []u8, nonce: []u8, aad: []u8, plain: []u8, out: &var String) -> std::mem::Error!void
std::crypto::chacha20_poly1305_open(allocator, key: []u8, nonce: []u8, aad: []u8, sealed: []u8, out: &var String) -> Failure!void
std::crypto::aes_gcm_seal(allocator, key: []u8, nonce: []u8, aad: []u8, plain: []u8, out: &var String) -> std::mem::Error!void
std::crypto::aes_gcm_open(allocator, key: []u8, nonce: []u8, aad: []u8, sealed: []u8, out: &var String) -> Failure!void

std::crypto::random_bytes(io: Io, allocator: Allocator, count: i64) -> Error!String
std::crypto::random_into(io: Io, allocator: Allocator, out: &var String, count: i64) -> Error!void

std::crypto::Hash     Sha256 | Sha384 | Sha512
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
`digest` の先頭に書きます。`sha512` と `sha384` は同じ標準の 64 bit word 版で、
64 byte と 48 byte の digest を書きます(P-384 の証明書の署名は SHA-384 を使います)。
`hmac_sha256` は RFC 2104 の HMAC で、`key` による `message` の 32 byte の tag を
`tag` の先頭に書きます。key はどんな長さでもよく、
1 block(64 byte)より長い key は先に hash され、短い key は 0 で埋められます。

結果は呼び手の buffer の view に書きます。`var out = [32]u8{}` と
`as_mut_bytes()` がその形で、hash は何も確保せず、失敗もしません。32 byte より
短い view は bounds trap です(index が末尾を越えたときと同じ)。

`Hash` は 3 つの hash 関数に付けた名前で、署名の検証のように、どの hash を使うかを
引数で受け取る API が使います。`digest_of(hash, bytes, digest)` はその名前で hash し、
`digest_length(hash)` はその digest の長さです。

message は 1 回の呼び出しで全部渡します。少しずつ渡す hasher はありません。
途中の block を struct に持たせる必要があり、stack buffer は struct field に
置けないからです(SPEC §7)。組み立て中のものを hash するなら、組み立ててから
渡します。

## 鍵導出

`hkdf_extract` と `hkdf_expand` は RFC 5869 の HKDF の 2 段です。`extract` は一様で
ない keying material(鍵交換の共有秘密)と salt から 32 byte の擬似乱数鍵を
`key` の先頭に書きます。salt が空なら、RFC 5869 §2.2 のとおり 32 byte の 0 を
salt にします。`expand` はその鍵と、用途を区別する `info` から、`out` の長さぶんの
bytes を書きます。長さは 255 block(8160 byte)までで、それより長い view は RFC が
出力を定めていない誤用なので trap します。block 番号を wrap させて同じ bytes を
繰り返し出すことはしません。
TLS 1.3 の key schedule はこの 2 段に TLS 自身の info を与えたものです。

## 鍵合意

`x25519` は RFC 7748 の X25519 で、`scalar`(自分の秘密鍵、32 byte)を `point`
(相手の公開鍵、32 byte)に掛けた 32 byte を `out` の先頭に書きます。`x25519_base`
は秘密鍵から公開鍵を作ります(base point u = 9 に掛けたもの)。双方がそれぞれ自分の
秘密鍵を相手の公開鍵に掛けると同じ 32 byte になり、その値は公開鍵だけからは
求められません。秘密鍵は `random_bytes` で引いた 32 byte そのままでよく、RFC の言う
clamp は中でします。

相手の point が低位数(u = 0、1、p - 1 など)なら結果は全 0 で、それは相手が共有
秘密を誰にでも分かるものにしたということなので、`LowOrderPoint` で拒否します
(RFC 7748 §6.1、TLS 1.3 は RFC 8446 §7.4.2 でこの検査を求めます)。長さが 32 でない
scalar や point は誤用で trap します。

体の算術は 16 bit の limb 16 本を `i64` で持つ TweetNaCl の形です。ladder の swap は
分岐ではなく mask で行い、処理時間は秘密鍵に依存しません。1 回の乗算は `--opt` で
1 ms 弱です(Go の assembly は 0.05 ms)。

## 署名

`ecdsa_p256_verify` は P-256(secp256r1)上の ECDSA 署名を検証します(FIPS 186-4
§6.4.2)。`public_key` は SEC 1 の非圧縮 point 65 byte(`04`、x、y)で、証明書の
SubjectPublicKeyInfo が持つ形そのものです。`signature` は r と s を 32 byte big-endian
で並べた 64 byte、`digest` は SHA-256 の 32 byte です。鍵や署名が別の形、鍵が曲線上に
ない、r や s が 1..n-1 の外、のどれも「不正な署名」であって bug ではないので false を
返します。digest が 32 byte でないのは呼び手の誤用で trap します。`ecdsa_p384_verify` は
同じことを P-384(secp384r1)と SHA-384 でします。鍵は 97 byte、r と s は 48 byte ずつ、
digest は 48 byte です。

署名の生成はありません。生成には秘密鍵と、1 度しか使ってはならない nonce の管理が
要り、今の利用者(TLS client と証明書の検証)はどちらも必要としないからです。DER の `SEQUENCE { INTEGER r, INTEGER s }` から r と s を
取り出すのは `std::crypto::x509`(`docs/std/x509.md`)の仕事です。

`rsa_pkcs1_verify` と `rsa_pss_verify` は RSA 署名を検証します(RFC 8017 §8.2.2 と
§8.1.2)。鍵は `modulus` と `exponent` を big-endian の byte で持ち、証明書の
SubjectPublicKeyInfo が持つ形そのものです。`hash` は署名の対象になった digest の
種類、`digest` はその digest、`signature` は modulus と同じ長さです。PSS は MGF1 に同じ
hash を使い、salt は digest と同じ長さで、TLS 1.3 と証明書はその形です。modulus が
偶数か 4096 bit より広いか padding に足りない、exponent が偶数か 32 bit より広い、
signature の長さが modulus と違うか値が modulus 以上、のどれも false です。digest の
長さが `hash` と合わないのは呼び手の誤用で trap します。

算術は 32 bit limb を `u64` で持つ Montgomery 乗算で、limb 数は modulus から決まり
(P-256 は 8 本、P-384 は 12 本、RSA は 4096 bit まで 128 本)、体 p、位数 n、RSA の
modulus に同じ code を使います。point は Jacobian 座標で、2 つの曲線とも a = -3 なので
同じ式が使え、u1 G + u2 Q は Straus–Shamir の同時 double-and-add です。扱うのは
公開鍵と署名だけで秘密は無いので、定数時間にはせず、値で分岐する速い実装です。1 回の検証は `--opt` で P-256 が
1 ms 弱、RSA-2048 が約 0.3 ms です。

## AEAD

`chacha20_poly1305_seal` は RFC 8439 の AEAD_CHACHA20_POLY1305 で、`plain` の
暗号文と 16 byte の tag を `out` の末尾に append します。`aad` は認証されるが暗号化
されない bytes です(TLS では record header)。
`chacha20_poly1305_open` は `sealed`(暗号文の後ろに tag)の tag を先に定数時間で
検査し、通ったものだけ復号して `out` に append します。合わなければ何も append
せず `Error::AuthenticationFailed` です。暗号文が改変されたのか、別の key / nonce /
aad で封じたものなのかは区別できないので、error も 1 種類です。

`aes_gcm_seal` / `aes_gcm_open` は NIST SP 800-38D の AES-GCM で、同じ形です。
key が 16 byte なら AES-128、32 byte なら AES-256。AES は table を引かず、S-box を
bitslice した word 上の論理回路(Boyar–Peralta)で計算し、GHASH の GF(2^128) 乗算は
bit を 4 つおきに広げた整数乗算で書いてあります(BearSSL の `aes_ct64` と
`ghash_ctmul64` の移植、MIT)。どちらも key や text の値で memory address や分岐が
変わらないので、処理時間から key が漏れません。table 引きの AES は cache timing で
key が漏れます。

ChaCha20-Poly1305 の key は 32 byte、nonce は 12 byte で、違う長さは誤用なので
trap します(AES-GCM も同様)。nonce は 1 つの key の下で 1 回だけ使います。同じ組で
2 つの message を封じると、2 つの平文の XOR が漏れ、GCM では認証鍵も漏れます。
nonce の決め方は protocol の仕事で(TLS は record の連番)、`std::crypto` は nonce を
生成しません。

## 比較

`equal_constant_time` は 2 つの view が同じ bytes かを、長さだけに依存する時間で
答えます。`std::mem::equal_bytes` は最初の不一致で止まるので、処理時間から先頭
何 byte が一致したかが分かります。受け取った tag をそれで比べると、攻撃者は tag を
1 byte ずつ当てられます。長さが違う 2 つは等しくなく、それは即座に決まります。
tag の長さは秘密ではありません。

## 乱数

`random_bytes` は host の乱数源から `count` byte を取得し、owner の String で返します。
native では kernel の entropy(`getentropy`)、WASI では `random_get`、browser では
`crypto.getRandomValues` です。`random_into` は呼び手の String の末尾に append し、
失敗したら String は元のままです。`count` が 0 以下なら何も取得しません。

取得には `Io` が要ります。乱数の取得は host への呼び出しなので、他の Io と同じく
呼び出しが source に見え、`std::testing::failing_io()` では `IoFailing` になります。
`std::rand` は seed から決まる擬似乱数で、test には向きますが鍵には使えません
(`docs/std/rand.md`)。

`std::crypto` は現在時刻を取得しません。証明書の有効期限のように現在時刻が必要な
検証は、呼び手が `std::process::unix_millis()` で取った値を引数で渡します。
現在時刻を取得する場所が呼び手の source に残り、test は固定の時刻を渡せます。

## 検証

test は公開されている test vector を使います。FIPS 180-4 の例、RFC 4231、RFC 5869、
RFC 8439、RFC 7748、RFC 6979 の case が `tests/behavior/src/crypto/` にあり、`examples/`
の crypto example が出力する hex は `cmd/kizu` の test が Go の実装の出力と比べます。
ECDSA と RSA は Go が生成した鍵で署名したものを Kizu が検証する test もあります。
