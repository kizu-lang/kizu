# std::tls

TLS 1.3(RFC 8446)の client です。今あるのは 3 つ: handshake の状態機械(その
遷移表は `spec/Tls.lean` が仕様として持ち、性質を証明し、trace を吐き、
`examples/tls_conformance.kizu` がそれを `std::tls` に流します)、key schedule、
record layer です。算術は全部 `std::crypto` で、C は link しません。

```text
std::tls::advance(state: State, event: Event) -> Transition

std::tls::hkdf_expand_label(secret: []u8, label: []u8, context: []u8, out: &var []u8) -> void
std::tls::derive_secret(secret: []u8, label: []u8, transcript_hash: []u8, out: &var []u8) -> void
std::tls::transcript_hash(transcript: []u8, out: &var []u8) -> void
std::tls::early_secret(out: &var []u8) -> void
std::tls::handshake_secret(early: []u8, shared: []u8, out: &var []u8) -> void
std::tls::master_secret(handshake: []u8, out: &var []u8) -> void
std::tls::traffic_key(secret: []u8, key: &var []u8, iv: &var []u8) -> void
std::tls::finished_verify_data(base_secret: []u8, transcript_hash: []u8, out: &var []u8) -> void

std::tls::seal_record(allocator, suite: CipherSuite, key: []u8, iv: []u8, sequence: i64, content_type: u8, content: []u8, out: &var String) -> std::mem::Error!void
std::tls::open_record(allocator, suite: CipherSuite, key: []u8, iv: []u8, sequence: i64, record: []u8, out: &var String) -> Failure!u8
std::tls::key_length(suite: CipherSuite) -> i64
std::tls::content_handshake() / content_application_data() / content_alert() -> u8

std::tls::CipherSuite   Aes128GcmSha256 | Chacha20Poly1305Sha256
std::tls::Error         BadRecord
std::tls::Failure       Error or std::crypto::Failure

std::tls::State     WaitServerHello | WaitEncryptedExtensions | WaitCertificateRequest |
                    WaitCertificate | WaitCertificateVerify | WaitFinished | Connected | Closed
std::tls::Event     ServerHello | HelloRetryRequest | EncryptedExtensions | CertificateRequest |
                    Certificate | CertificateVerify | Finished | NewSessionTicket | KeyUpdate |
                    ApplicationData | CloseNotify | FatalAlert
std::tls::Outcome   Applied | Refused | Unsupported
std::tls::Transition { state: State, outcome: Outcome }
```

## 状態機械

接続は ClientHello を送った `WaitServerHello` から始まり、server の message を
RFC 8446 §A.1 の順に受けます。表に無い message は `Refused` で、client は
`unexpected_message` alert を送って `Closed` になります。`HelloRetryRequest` は
この client が扱わない message で、`Unsupported`(`handshake_failure`)です。
`close_notify` と fatal alert はどの状態でも `Closed` に移り、`Closed` では何も
起きません。

`spec/Tls.lean` が証明しているのは、閉じた接続は閉じたまま(`closed_absorbing`)、
`Connected` になるのは `WaitFinished` での `Finished` だけ
(`connected_only_by_finished`)、application data を受けるのは `Connected` だけ
(`data_needs_connection`)、`CertificateVerify` は `Certificate` の後にだけ
(`verify_needs_certificate`、`certificate_first`)、拒否した接続は閉じる
(`refusal_closes`)、です。

client は PSK も early data も出さず、自分の証明書も持ちません。server の
`CertificateRequest` は空の `Certificate` で答えます。

## key schedule

RFC 8446 §7.1 の HKDF の段です。`early_secret` は PSK の無い handshake の
Early Secret、`handshake_secret` は鍵交換の共有秘密(`std::crypto::x25519`)を
その派生 salt で extract したもの、`master_secret` はさらにその派生 salt で
0 を extract したものです。traffic secret は `derive_secret(secret, "c hs traffic", hash)`
のように label と transcript hash から作り、`traffic_key` がそれを AEAD の key と
12 byte の IV に、`finished_verify_data` が Finished の 32 byte にします。

transcript は handshake message を並べた bytes をそのまま持ち、要るたびに
`transcript_hash` で丸ごと hash します。数 KB を数回 hash するだけで、途中の
block を struct に抱える hasher は要りません。

## record layer

RFC 8446 §5 の暗号化された record です。`seal_record` は content の末尾に本当の
type を付けて AEAD で封じ、5 byte の header(type は常に `application_data`、
version は 0x0303)を associated data にし、nonce は IV と sequence number の
XOR です。`open_record` はその逆で、padding の 0 を剥がして type を返します。
header が TLS 1.3 のものでない、tag が合わない、type が無い record は
`BadRecord` です。content は 2^14 byte まで(長いものは複数 record)。

RFC 8448 §3 の handshake の secret、key、record は `tests/behavior/src/tls/` が
全部確かめています。server の handshake record はその 4 つの message に開き、
client の Finished と application data と alert は byte 単位で同じ record に
封じられます。

## 今は話さないこと

- handshake message の encode / decode(ClientHello / ServerHello / Certificate /
  CertificateVerify / Finished)と、それを状態機械・key schedule・record layer・
  `std::crypto::x509` と繋いで socket に流すこと
- `TLS_AES_256_GCM_SHA384`(SHA-384 が無い)
- `HelloRetryRequest`、PSK / session resumption、0-RTT、client 証明書、KeyUpdate の送信
- server 側
