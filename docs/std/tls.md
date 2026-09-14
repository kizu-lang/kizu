# std::tls

TLS 1.3(RFC 8446)の client です。今あるのは handshake の状態機械だけで、
その遷移表は `spec/Tls.lean` が仕様として持ち、性質を証明し、trace を吐きます。
`examples/tls_conformance.kizu` がその trace を `std::tls` に流します。

```text
std::tls::advance(state: State, event: Event) -> Transition

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

## 今は話さないこと

- record layer(暗号化、`std::crypto` の AEAD で)、key schedule(HKDF)、
  handshake message の encode / decode、transcript hash
- server 証明書の検証を handshake に繋ぐこと(`std::crypto::x509` はある)
- `HelloRetryRequest`、PSK / session resumption、0-RTT、client 証明書
- server 側
