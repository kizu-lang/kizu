# std::tls

TLS 1.3(RFC 8446)の client と server です。`connect` が TCP 接続の上で client
として、`accept` が server として handshake を終え、`Connection` が application
data を読み書きします。構成は 4 つです。handshake の状態機械(client と server の
遷移表は `spec/Tls.lean` が仕様として持ち、性質を証明し、client の trace を出力
します。`examples/tls_conformance.kizu` がその trace を `std::tls` で再生します)、
key schedule、record layer、handshake message の codec です。証明書の検証と鍵 file
は `std::crypto::x509`、算術は全部 `std::crypto` で、C は link しません。

```text
std::tls::connect(io: Io, allocator, address: []u8, host: []u8, roots: &Array<String>, now: i64) -> Failure!Connection
std::tls::accept(io: Io, allocator, stream: std::net::TcpStream, chain: &Array<String>, key: &x509::PrivateKey) -> Failure!Connection
Connection.write(io, allocator, bytes: []u8) -> Failure!void
Connection.read_into(io, allocator, out: &var String, max: i64) -> Failure!i64   // 0 は相手の close_notify
Connection.read_ready(io, allocator, out: &var String, max: i64) -> Failure!?i64
Connection.close(io, allocator) -> Failure!void
Connection.cipher_suite() -> CipherSuite
Connection.server_name() -> []u8        // server 側: client が server_name で求めた host
Connection.deinit(allocator)

std::tls::advance(state: State, event: Event) -> Transition
std::tls::advance_server(state: ServerState, event: ServerEvent) -> ServerTransition

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

std::tls::write_client_hello / write_finished / write_empty_certificate / write_key_update
std::tls::write_server_hello / write_encrypted_extensions / write_certificate / write_certificate_verify
std::tls::read_server_hello / read_certificate / read_certificate_verify / read_finished / read_key_update
std::tls::read_client_hello(message: []u8) -> Error!ClientHello
std::tls::message_length(bytes: []u8) -> ?i64

std::tls::CipherSuite   Aes128GcmSha256 | Chacha20Poly1305Sha256
std::tls::Error         BadRecord | IllegalParameter | Unsupported | UnexpectedMessage | BadSignature | Alert | Closed
std::tls::Failure       Error or std::crypto::Failure or std::crypto::x509::Error or std::net::Error

std::tls::State     WaitServerHello | WaitEncryptedExtensions | WaitCertificateRequest |
                    WaitCertificate | WaitCertificateVerify | WaitFinished | Connected | Closed
std::tls::Event     ServerHello | HelloRetryRequest | EncryptedExtensions | CertificateRequest |
                    Certificate | CertificateVerify | Finished | NewSessionTicket | KeyUpdate |
                    ApplicationData | CloseNotify | FatalAlert
std::tls::Outcome   Applied | Refused | Unsupported
std::tls::Transition { state: State, outcome: Outcome }

std::tls::ServerState   WaitClientHello | WaitFinished | Connected | Closed
std::tls::ServerEvent   ClientHello | EndOfEarlyData | Certificate | CertificateVerify | Finished |
                        KeyUpdate | ApplicationData | CloseNotify | FatalAlert
std::tls::ServerTransition { state: ServerState, outcome: Outcome }
```

```kizu
var client = try tls::connect(io, allocator, "127.0.0.1:443", "example.com", &roots, now);
defer client.deinit(allocator);
try client.write(io, allocator, "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n");
var reply = string::new(allocator);
defer reply.deinit(allocator);
while try client.read_into(io, allocator, &var reply, 4096) > 0 {}
try client.close(io, allocator);

// server: chain は証明書の DER(自分のが先頭)、key は x509::parse_private_key の結果
let stream = try listener.accept(io);
var connection = try tls::accept(io, allocator, move stream, &chain, &key);
defer connection.deinit(allocator);
```

## 接続

`connect` は ClientHello(TLS 1.3 だけ、`TLS_AES_128_GCM_SHA256` と
`TLS_CHACHA20_POLY1305_SHA256`、x25519、`ecdsa_secp256r1_sha256`、
`ecdsa_secp384r1_sha384`、`rsa_pss_rsae_sha256` / 384 / 512(証明書には
`rsa_pkcs1_*` も)、server_name)を
送り、server の message を状態機械の順に受けます。ServerHello で共有秘密と
handshake の鍵を作り、Certificate は `x509::verify_chain(chain, roots, host, now)` で
検証し、CertificateVerify は transcript hash への署名を証明書の鍵で検証し、Finished
を確かめてから自分の Finished を送り、application の鍵に切り替えます。どこかで
失敗したら RFC が定める alert(`unexpected_message`、`illegal_parameter`、
`bad_certificate` / `unknown_ca` / `certificate_expired`、`decrypt_error`、
`handshake_failure`)を送って閉じ、その理由を error で返します。

`read_into` は application data を渡し、途中の NewSessionTicket は捨て、KeyUpdate は
鍵を更新して(求められれば自分も送って)続けます。相手の close_notify は 0 で、
それ以外の alert は `Alert` です。`close` は close_notify を送って書き込み側を閉じます。
`read_ready` は待たない版で、record が丸ごと届いていなければ null です。

## server

`accept` は listener が受けた `TcpStream` の上で server として handshake します。
ClientHello を読み、suite(client が挙げた中で AES-128-GCM を優先)、x25519 の
share、鍵で署名できる scheme(P-256 は `ecdsa_secp256r1_sha256`、P-384 は
`ecdsa_secp384r1_sha384`、RSA は `rsa_pss_rsae_sha256` / 384 / 512 の順)を決め、
ServerHello、change_cipher_spec(§D.4)、EncryptedExtensions(空)、Certificate
(`chain` の DER をそのまま)、CertificateVerify(`std::crypto::x509::sign`)、Finished
を送って client の Finished を待ちます。TLS 1.3 を offer しない client には
`protocol_version`、suite / x25519 share / scheme が無い client には
`handshake_failure` を送って閉じます(HelloRetryRequest は送りません)。client 証明書
は求めず、session ticket は発行しません。`server_name` は client が SNI で求めた host
で、virtual host の選択は呼び手の仕事です。

乱数は `std::crypto::random_bytes` から取得し、時刻は呼び手の `now` を使います。
`std::tls` 自身は現在時刻も乱数も取得しません。

## 状態機械

client の接続は ClientHello を送った `WaitServerHello` から始まり、server の message を
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

client は PSK も early data も送らず、自分の証明書も持ちません。server の
`CertificateRequest` は空の `Certificate` で答えます。

server の機械は `WaitClientHello` → (自分の flight を送って)`WaitFinished` →
`Connected` の 3 段で、`advance_server` が動かします。client の `Certificate` /
`CertificateVerify`(求めていない)と `EndOfEarlyData`(受けていない)は拒否です。
`spec/Tls.lean` の `serverTable` と、`Connected` に至るのは待っていた `Finished` だけ
(`server_connected_only_by_finished`)、`WaitFinished` に至るのは `ClientHello` だけ
(`server_wait_finished_by_hello`)、証明書は決して受けない
(`server_takes_no_certificate`)、の証明が対応します。表の全 36 組は
`tests/behavior/src/tls/` が Lean の表と突き合わせます。

## key schedule

RFC 8446 §7.1 の HKDF の段です。`early_secret` は PSK の無い handshake の
Early Secret、`handshake_secret` は鍵交換の共有秘密(`std::crypto::x25519`)を
その派生 salt で extract したもの、`master_secret` はさらにその派生 salt で
0 を extract したものです。traffic secret は `derive_secret(secret, "c hs traffic", hash)`
のように label と transcript hash から作り、`traffic_key` がそれを AEAD の key と
12 byte の IV に、`finished_verify_data` が Finished の 32 byte にします。

transcript は handshake message を並べた bytes をそのまま持ち、要るたびに
`transcript_hash` で丸ごと hash します。数 KB を数回 hash するだけで、途中の
block を struct に持つ hasher は要りません。

## record layer

RFC 8446 §5 の暗号化された record です。`seal_record` は content の末尾に実際の
content type を付けて AEAD で暗号化し、5 byte の header(type は常に `application_data`、
version は 0x0303)を associated data にし、nonce は IV と sequence number の
XOR です。`open_record` はその逆で、padding の 0 を剥がして type を返します。
header が TLS 1.3 のものでない、tag が合わない、type が無い record は
`BadRecord` です。content は 2^14 byte まで(長いものは複数 record)。

RFC 8448 §3 の handshake の secret、key、record は `tests/behavior/src/tls/` が
全部確かめ、server が書く ServerHello と Certificate は RFC の bytes と一致します。
`cmd/kizu` の test は Go の `crypto/tls` の server と実際に handshake して echo し、
名前の違う host と別の root では拒否されることを見ます。server 側は Go の client が
P-256 / P-384 / RSA の証明書で接続して echo し、別の root を信頼する client には
拒否されることを見ます。

## まだ無いもの

- `TLS_AES_256_GCM_SHA384`(key schedule が SHA-256 固定)
- `HelloRetryRequest`、PSK / session resumption、0-RTT、client 証明書、
  自分から送る KeyUpdate、ALPN
- server の SNI による証明書の選択(`server_name` を見て呼び手が選ぶことはできる)
- 鍵と証明書の公開鍵が合っているかの確認(合っていなければ client が
  `decrypt_error` で拒否する)
