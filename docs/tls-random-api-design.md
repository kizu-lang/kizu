# std::tls のテストを録った transcript の再生に替える(設計)

`cmd/kizu` の TLS 系テスト 3 本(`tls_client_test.go` / `tls_server_test.go` /
`http_tls_test.go`)は、Go の `crypto/tls` と `net/http` を接続相手にしている。これを
「外部実装と一度だけ接続して録った transcript を checked-in の fixture にし、それを
再生する」形に替えるための設計です。再生には Kizu 側が送る bytes を毎回同じにする
必要があり、そのために std::tls へ乱数の口を足します。
決まった部分は実装と同時に `docs/std/tls.md` / `docs/std/http.md` と各ファイルの
コメントへ移し、この文書は役目を終えたら消します。

## 0. 要約

| 項目 | 推奨 |
| --- | --- |
| 録る相手 | devShell の node(`pkgs.nodejs`、v20.18.1 / OpenSSL 3.0.14。flake.lock で固定) |
| 録り方 | node の script が相手の TLS 端点と TCP relay を兼ね、両方向の record を順に記録する。録り直しは `nix develop -c just tls-fixtures` の 1 経路(§2.4) |
| 形式 | 1 接続 1 ファイルの text。1 行 1 record(向き + hex)、先頭に metadata と sha256 |
| 置き場 | `examples/fixtures/tls/`(`compress` と同じ流儀)。録る道具は `scripts/record-tls-fixtures/` |
| 乱数の口 | `std::tls::Entropy` を接続に持たせる。既定は今と同じ OS の乱数、再生用は seed から決定的に出す `predictable_entropy` |
| 乱数の網羅 | handshake の乱数 3 箇所すべてを `Entropy` から引く。OS の乱数を直接引く経路は残さない(§3.3) |
| 再生の場所 | `tests/behavior/src/tls_replay/`。evented Io 上で再生役の task と Kizu 側を同じ process で動かす |
| 判定 | Kizu が送った record が録ったものと byte 単位で一致すること、および Kizu 側の結果(出力・error 名) |
| PR | (A) std の口だけ足す (B) 録る道具・fixture・再生 module を足し、Go のテスト 3 本を消す |

比較相手が Kizu 以外である点は保つ。fixture の中の Kizu の bytes は node が受け入れた
bytes そのものなので、「再生で一致した」は「node がもう一度受け入れる」と同じ意味になる。
相手の bytes は録った時点で AEAD に守られているので、Kizu がそれを開けて検証できる
こと自体が相手の実装に対する検査になる。

## 1. 現状(根拠)

### 1.1 接続ごとに変わる入力

| # | 入力 | 場所 | bytes に効くか | 固定の仕方 |
| --- | --- | --- | --- | --- |
| 1 | client の乱数 96 byte(hello random 32、session_id 32、x25519 秘密鍵 32) | `lib/kizu/std/src/tls/client.kizu:38` | 効く | `Entropy`(§3) |
| 2 | server の乱数 64 byte(hello random 32、x25519 秘密鍵 32) | `server.kizu:112` | 効く | `Entropy` |
| 3 | server の CertificateVerify の salt(scheme の digest 長) | `server.kizu:284` | RSA-PSS で効く | `Entropy`。鍵の種類によらず引き、読むのは RSA だけ。ECDSA の署名自体は RFC 6979 の決定的 nonce で乱数を使わない(`crypto/ecdsa.kizu:176`) |
| 4 | 時刻 `now` | `tls::connect` の引数。`http::connect_secure` は `process::unix_millis()` を自分で読む(`http/client.kizu:77`) | 検証の結果を通して効く(alert か Finished か) | `tls_client` は定数を渡す。http client は実時計なので、証明書の有効期間を 9999-12-31 まで取る |
| 5 | host(SNI)、roots、server の chain と鍵 | 呼び手の引数 | 効く | 固定の証明書と鍵(§2.3) |
| 6 | Kizu 側が書く application data とその分割(16384 byte ごと、`connection.kizu:881`) | テストのプログラム | 効く | プログラムで固定 |
| 7 | http client の `Host` header にある port | `http/client.kizu:508`(`url.authority`) | 効く(application data の中) | 実行ごとに変わる ephemeral port。§4.1 の置換付き比較で扱う |
| 8 | 受信に応じて送るもの(相手の KeyUpdate への応答、post-handshake message の処理) | `connection.kizu` `take_record` | 効く | 相手の bytes を固定し、録った順に再生する(§4.1) |
| 9 | 相手側の乱数(node の hello random、key share、NewSessionTicket) | ― | 相手の bytes | fixture に録られて固定される |
| 10 | http server の deadline(`process::monotonic_millis`、`http/server.kizu:766`) | ― | 時間切れが起きない限り効かない | 既定の limits のまま。再生は数 ms で終わる |

無いもの(コードで確認): session ticket の発行、resumption / PSK / 0-RTT、Kizu から
始める KeyUpdate、Kizu server からの HelloRetryRequest、std::tls 内での時計の読み取り、
http の `Date` header(client も server も送らない)。ALPN は Kizu が送らず扱わない。

std::tls は `lib/kizu/std/src/tls/` の Kizu コード 1 本で、二重実装は無い。両 compiler
に入っているのは乱数の builtin(`crypto_random_into`)までなので、§3 の口は std の
Kizu コードだけの変更で済み、Go 実装と `compiler/` の両方に入れる変更は生じない。

### 1.2 現行の case(16)

| テスト | case | 期待 |
| --- | --- | --- |
| `TestTlsClientTalksToGoServer` | echo(P-256) | `connected with AES-128-GCM` / `HELLO OVER TLS` / `closed` |
| 〃 | wrong host(`example.test`) | `NameMismatch` |
| 〃 | wrong root | `UnknownIssuer` |
| 〃 | echo over P-384 | echo と同じ |
| 〃 | echo over RSA(2048) | echo と同じ |
| `TestTlsServerTalksToGoClient` | 256 / 384 / 2048 | Go が `HELLO OVER TLS` を読み、server は `served localhost with AES-128-GCM` / `hello over tls` / `closed` |
| 〃 | Kizu client(両端 Kizu) | 両方の出力 |
| 〃 | wrong root | server が `Alert` か `ConnectionReset`(kernel の timing 次第) |
| `TestHttpClientFetchesOverTls` | fetch / wrong root / wrong host(`127.0.0.1`) / no roots | `200` + 本文 / `UnknownIssuer` / `NameMismatch` / `NoTrustedRoots` |
| `TestHttpServerServesOverTls` | accept / loop | 1 本の keep-alive 接続で `/one` `/two`、新しい接続で `/three`、別 root の client は handshake で拒否。server は `served /one` `/two` `/three` |

証明書は毎回 Go が生成している(`selfSignedServer`、有効期間は実行時刻 ±)。

## 2. fixture の録り方

### 2.1 相手の実装

| 候補 | 入手 | version 固定 | 書き方 | keylog | 判断 |
| --- | --- | --- | --- | --- | --- |
| **node `tls` / `https`** | devShell にある | flake.lock で固定(v20.18.1、OpenSSL 3.0.14) | script で client も server も、echo も http も書ける | `keylog` event | **推奨** |
| `openssl s_server` / `s_client` | devShell に無い。macOS の `/usr/bin/openssl` は LibreSSL 3.3.6 で CI と別物 | 足せば固定できるが devShell が増える | echo を大文字にする等の振る舞いが書けない。http は `-WWW` 程度 | OpenSSL のみ `-keylogfile`(LibreSSL は無い) | 不採用 |
| Go `crypto/tls` | ある | ある | 自由 | ある | 外すのが目的なので不可 |

node は PR #1996 でも鍵 file の writer として使っている。録り直しも `nix develop` の中で
同じ version が手に入る(固定の仕方と録り直しの運用は §2.4)。

### 2.2 録る接続の構成

録る道具(`scripts/record-tls-fixtures/record.mjs`)は 1 接続ごとに次を組む。

```text
Kizu 側(go run ./cmd/kizu run …、predictable_entropy で seed を固定)
   │ TCP
relay(node、両方向の bytes を TLS record 単位で順に記録)
   │ TCP
相手の TLS 端点(node tls / https、keylog を記録)
```

relay は Kizu から相手へ、相手から Kizu へ流れた record を到着順に 1 本の列にする。
Kizu の record は、それを送らせた相手の record を relay が転送した後にしか relay に
届かないので、この列は Kizu から見て因果順になっている(§4.1 の再生が詰まらない根拠)。

| fixture | Kizu 側 | 相手 | 対応する現行 case |
| --- | --- | --- | --- |
| `client-echo-p256` / `-p384` / `-rsa` | `tls::connect_with` で `hello over tls` を送り、返事を読み、close | node tls server(大文字で echo) | tls_client の echo 3 つ |
| `client-wrong-host` | host `example.test` | 同上(P-256) | wrong host |
| `client-wrong-root` | roots が別の root | 同上 | wrong root |
| `server-echo-p256` / `-p384` / `-rsa` | `tls::accepting_with` + `accept_wait`、1 行読んで大文字で返し close | node tls client | tls_server の 256 / 384 / 2048 |
| `server-wrong-root` | 同上 | 別の root を信じる node client | tls_server の wrong root |
| `https-client-fetch` / `-wrong-root` / `-wrong-host` | `http::connect_secure_with` で GET | node https server | http client の 3 つ |
| `https-server-keepalive` / `-fresh` / `-stranger` | `http::listen_secure_with` の server | node https client(keep-alive の agent、新しい agent、別 root の agent) | http server(accept と loop の両方がこの 3 本を再生する) |

http_tls は client 側・server 側とも自分の fixture を持つ。tls_client / tls_server の
fixture を流用しないのは、HTTP を載せると application data が違い、`Host` の扱い(§4.1)
も要るため。`no roots` は接続の前に `fetch_with` が返す(`http/client.kizu:453`)ので
fixture は要らない。両端 Kizu の case も fixture を持たない(§4.2)。

録る道具は相手側の結果(handshake が通ったか、alert の種類、読んだ本文)を header に
書き、現行テストの期待と違えばファイルを書かずに失敗する。期待どおりに相手が受け入れた
接続だけが fixture になる。

### 2.3 固定する条件

| 条件 | 値 |
| --- | --- |
| 相手 | node v20.18.1 / OpenSSL 3.0.14(header に記録) |
| version | TLS 1.3 のみ(node 側 `minVersion` / `maxVersion`) |
| 鍵共有 | X25519(node 側 `ecdhCurve: 'X25519'`。Kizu server は HRR を送らない) |
| cipher suite | 指定しない(Kizu の offer は AES-128-GCM と ChaCha20 の 2 つ。node は現行どおり AES-128-GCM を選ぶ)。選ばれた suite を header に記録 |
| ALPN | なし(両側とも設定しない) |
| 証明書と鍵 | `examples/fixtures/tls/` に固定で置く: `localhost-p256` / `-p384` / `-rsa2048`(自己署名、CA、SAN `DNS:localhost`)と、無関係な root `other-p256`。有効期間 2026-01-01 〜 9999-12-31。PR #1996 の chain と同じく LibreSSL 3.3.6 の `openssl` で一度だけ作り、作り方をファイル冒頭のコメントに書く |
| `now` | tls_client は 1893456000(2030-01-01)を渡す。http は実時計のまま(上の有効期間で足りる) |
| Kizu の seed | case 名から決める(`SHA-256("kizu tls fixture " + case)`)。録り直しても Kizu の最初の flight が変わらないので、diff に意図した変更だけが出る |
| Kizu が書く data | 現行と同じ `hello over tls`、route `/greeting` `/one` `/two` `/three` |

### 2.4 再生成の経路

fixture は一度録って終わりにせず、std::tls の wire が変わるたびに同じ手順で録り直す。
Go の `crypto/tls` が `-update` flag で testdata を書き直すのに当たる運用を、Go の外で
1 本の経路として持つ。

| 項目 | 決め方 |
| --- | --- |
| 道具 | `scripts/record-tls-fixtures/record.mjs` と Kizu 側のプログラム(§2.6)。冒頭のコメントに目的・実行方法・録る case の一覧を書く |
| 実行 | `nix develop -c just tls-fixtures`。引数を取らず、§2.2 の 15 本を毎回全部録り直す(1 本だけ録り直す経路は持たない。fixture ごとに相手の version が混ざらない) |
| 相手の version の固定 | node は devShell の `pkgs.nodejs` で、version は `flake.lock` の nixpkgs の rev が決める。record.mjs は起動時に `process.versions.node` と `process.versions.openssl` を読み、`process.execPath` が `/nix/store/` の下にあることを確かめる(devShell の外の node では録らない) |
| version の記録先 | 各 fixture の `# peer:`(node と OpenSSL の version)と `# recorder:`(§2.5)。この文書と `docs/std/tls.md` には「録った相手は fixture の `# peer:` を見る」とだけ書き、version の数字を二重に持たない。再生テストは全 fixture の `# peer:` が同じ 1 つであることを確かめる |
| 同じ相手を再現する | fixture を足した commit を checkout して `nix develop` に入れば、その `flake.lock` が同じ node を返す |
| CI | 録らない。再生は node を必要とせず、`go test` も CI も fixture を読むだけ |

録り直しが要るとき: Kizu が送る bytes が変わる変更。message の書式・拡張の集合や順序・
offer する suite や scheme・alert の選び方・record の切り方・`Entropy` から引く量と順序。
fixture の証明書や鍵を替えたとき、case を足したとき。

要らないとき: bytes を変えないリファクタ、`flake.lock` の更新で node の version が
変わったとき(fixture は録った時点で凍結されている)。次に録り直すときは新しい version で
15 本全部が録られ、`# peer:` の diff に version の変化が出る。

std::tls の wire 形式を変える PR の手順:

1. 再生テストが「`client-echo-p256` の 0 番目の `>` record が違う」の形で落ち、
   「意図した変更なら `nix develop -c just tls-fixtures` で録り直す」と出す。
2. 変更が意図したものなら、`nix develop` の中で `just tls-fixtures` を実行する。
   録る道具は相手が受け入れなければ書かないので、node に拒まれる変更はここで止まる。
3. 再生テストを流して通ることを確かめる。
4. Kizu の変更と同じ PR に、別 commit で fixture を入れる。commit message に録り直した
   理由を書く。
5. review では fixture の diff を読む。seed が case 名で固定なので、平文の record
   (ClientHello / ServerHello)の行の差分と、各 record に付く注釈(§2.5、復号した
   handshake message の種類と長さ)の差分が、意図した変更と対応しているかを見る。
   相手の乱数は録るたびに変わるので、相手の行と暗号化された行が全部変わるのは正常。

### 2.5 形式

| 候補 | diff | 改竄・欠落 | 判断 |
| --- | --- | --- | --- |
| 向きごとの生 binary(`.bin` 2 本) | 読めない | record 境界も向きの順序も失う | 不採用 |
| JSON | 読めるが 1 record が長い 1 文字列 | schema で検出 | 再生側に JSON を読む手間。不採用 |
| `.kizu` に hex を埋め込む(`crypto_vectors` の流儀) | 読める | ― | 生成物と手書きの test が 1 ファイルに混ざる。不採用 |
| **1 接続 1 text ファイル、1 行 1 record** | 行単位で読める | sha256 と record 長で検出 | **推奨** |

```text
# kizu tls trace 1
# case: client-echo-p256
# peer: node v20.18.1 OpenSSL 3.0.14, tls server, echo upper
# recorder: scripts/record-tls-fixtures/record.mjs (regenerate: nix develop -c just tls-fixtures)
# kizu: client host=localhost roots=localhost-p256 now=1893456000
# seed: 5f2c…(64 hex)
# suite: TLS_AES_128_GCM_SHA256
# keylog: CLIENT_TRAFFIC_SECRET_0 9a41…
# keylog: SERVER_TRAFFIC_SECRET_0 03be…
# outcome: peer handshake ok; peer read "hello over tls"
# sha256: 1c0e…
> 16030100b8 010000b4 0303 …        ; ClientHello 180
< 160303007a 0200007600 …           ; ServerHello 118
< 1403030001 01                     ; change_cipher_spec
< 1703030017 …                      ; EncryptedExtensions 6
< 17030301e8 …                      ; Certificate 471
…
> 1703030025 …                      ; application_data 14
```

- `# peer:` と `# recorder:` は生成元。この 2 行で、誰が何の version で録り、どう録り直すかが
  ファイル単体で分かる。
- `>` は Kizu が送った record、`<` は相手が送った record。hex は record header 込みの全体。
- `;` 以降は注釈。録る道具が keylog の secret で復号して(node の `crypto` の HKDF と
  AEAD で、Kizu とは独立に)内側の種類と長さを書く。再生は注釈を読まない。
- http client の fixture だけ `# port:` に録ったときの port を持つ(§4.1)。
- 改竄と欠落: `# sha256:` は自分の行を除いたファイル全体の SHA-256。再生側が
  std::crypto(公開 vector で検証済み)で計算し直して比べる。各行は record header の
  長さと hex の長さが合うことも確かめる。相手の record は AEAD で守られているので、
  1 byte でも変われば Kizu が `BadRecord` で落とす。最後の record まで消費したことと、
  Kizu が余分な bytes を送らずに閉じたことも判定に入れる。
- 大きさ: P-256 の接続で約 1.8 KB(Kizu の ClientHello 約 185 byte、node の flight 約
  0.9 KB、NewSessionTicket 2 通約 0.5 KB、残りは application data と close)、RSA で
  約 2.5 KB。hex と注釈で 1 本 4〜6 KB、15 本で 60〜90 KB、証明書と鍵が数 KB。

### 2.6 置き場と命名

| 候補 | 判断 |
| --- | --- |
| **`examples/fixtures/tls/`** | **推奨**。behavior test が repository root からの相対 path で読む前例がある(`tests/behavior/src/compress/compress_test.kizu:17` が `examples/fixtures/compress/` を読む)。`examples/fixtures/tls_trace.json` も TLS の生成物 |
| `cmd/kizu/testdata/` | Go のテストの置き場。Go から離す目的に反する |
| `tests/fixtures/` | module 読み込みのテスト用(`tests/fixtures/modules`) |

- trace: `examples/fixtures/tls/<側>-<case>.trace`(`client-echo-p256.trace` など §2.2 の 15 本)
- 証明書と鍵: `examples/fixtures/tls/<名前>.pem` と `<名前>.key.pem`
- 録る道具: `scripts/record-tls-fixtures/record.mjs` と、Kizu 側のプログラム
  (`client.kizu` / `server.kizu` / `https_client.kizu` / `https_server.kizu`)。
  `scripts/gen-compress-fixtures` と同じく package comment に使い方を書く
- just: `tls-fixtures`(`compress-fixtures` と並べる)

## 3. 乱数の口

### 3.1 案の比較

| | 案1a: 乱数源を `&var` で渡す | **案1b: 乱数源を接続に持たせる** | 案2: 値を接続ごとに渡す | 案3: 設定 struct に足す | 案4: Io に持たせる |
| --- | --- | --- | --- | --- | --- |
| 形 | `accept_ready(…, entropy: &var Entropy)` | `connect_with(…, entropy: Entropy)`、`accepting_with(…, entropy)` | `connect_with(…, random, session_id, key_share_secret)` | `tls::ClientConfig { roots, now, entropy }` | `io::predictable(...)` が乱数を返す Io |
| 表面積 | 既存の `accept_ready` / `accept_wait` の引数が変わるか、`_with` が倍に増える | 型 1、関数 7(下記。http の 2 つを含む)。既存の関数はそのまま | 引き方の内訳(96 = 32+32+32)が公開 API に出る。鍵共有を増やせば API が壊れる | 既存の呼び出しが全部壊れる | tls / http は無変更 |
| 通常利用 | 何も渡さない | 何も渡さない | 何も渡さない | 毎回 struct を組む | 何も渡さない |
| http server | 不可。`http::Exchange` は worker に move され(`io::spawn<http::Exchange>`)、handshake は Server を見ずに進む(`http/server.kizu:1102`) | Server が持つ源から接続ごとに `split` して Exchange に渡せる | 接続数が事前に分からず渡せない | 1b と同じ | 可 |
| 誤用の危険 | 1b と同じ | 予測できる源は `predictable_entropy` という 1 語でしか作れない(原理 3) | 名前に危険が出ない | 1b と同じ | program 全体の乱数が予測可能になる |
| 実装 | std のみ | std のみ | std のみ | std のみ | 両 compiler の runtime と wasm、SPEC の Io の契約 |

推奨は **案1b**。案1a は http server に届かない。案2 は内部の引き方を公開 API に固定する。
案3 は利用者全員の呼び出しを書き換えさせる(原理 8 と 10)。案4 は危険の範囲が program
全体に広がるうえ、テストのために言語の capability を変えることになる。

### 3.2 推奨案の API

```text
std::tls::Entropy                                     // 接続が乱数を引く源。複製できない
std::tls::system_entropy() -> Entropy                 // OS の乱数(今の振る舞い)
std::tls::predictable_entropy(seed: []u8) -> Entropy  // seed から決定的に出す。テストの再生用
Entropy.split() -> Entropy                            // 1 接続分の子。system は system を返す
std::tls::connect_with(io, allocator, address, host, roots, now, entropy: Entropy) -> Failure!Connection
std::tls::accepting_with(allocator, stream: TcpStream, entropy: Entropy) -> Connection
std::http::connect_secure_with(io, allocator, address, host, roots, limits, entropy: tls::Entropy) -> Failure!Connection
std::http::listen_secure_with(io, allocator, address, chain, key, limits, entropy: tls::Entropy) -> Failure!Server
```

- `Connection` が `Entropy` を持ち、client は handshake で 96 byte、server は ClientHello
  を受けたとき 64 byte と CertificateVerify の salt を、そこから引く(§3.3)。`accept_ready` /
  `accept_wait` の引数は変わらない。
- 既存の `connect` / `accept` / `accepting` / `http::connect_secure` / `listen_secure` は
  `system_entropy()` を渡す `_with` を呼ぶだけになる。経路は 1 本のまま(原理 9)。
- `predictable_entropy` は seed(長さは問わず、SHA-256 で 32 byte に畳む)を鍵にした
  HMAC-SHA256 の counter mode で bytes を出す。std::crypto の検証済み HMAC を使う。
  上限が無いので「使い切った」という error は要らない。`split` は親の seed と通し番号から
  子の seed を作る。http server は accept した順に `split` するので、接続を 1 本ずつ
  順に張る限り各接続の乱数は決まる。
- 複製できないこと: 同じ列を 2 つの接続が使うと nonce と鍵の再利用になる。Kizu の
  型規則で owner にする(`deinit` は持たせない形が取れるかは実装時に確かめる)。
- 置き場は std::tls。std::crypto に一般の源として出すのは実需が出てから(原理 8)。
  `std::rand`(seed 付きの非暗号 PRNG)とは用途が違うので相乗りしない(原理 7)。
- `docs/std/tls.md` に「`predictable_entropy` の接続は秘密を守らない。テストの再生
  以外で使わない」と書く。SPEC は変えない(compiler が知る契約ではない)。

通常利用(変わらない):

```kizu
var client = try tls::connect(io, allocator, address, "example.com", &roots, now);
```

再生(テスト側):

```kizu
let seed = try trace.seed();
var client = try tls::connect_with(
    io, allocator, address, "localhost", &roots, 1893456000,
    tls::predictable_entropy(seed));
```

```kizu
var connection = tls::accepting_with(allocator, move stream, tls::predictable_entropy(seed));
defer connection.deinit(allocator);
try connection.accept_wait(io, allocator, &chain, &key);
```

### 3.3 Entropy が賄う範囲

Go の `tls.Config.Rand` は非推奨になった。差し替えた源が handshake の乱数消費を全部
賄う保証が無く、源を差しても決定的にならない経路が残ったのが理由です。同じ穴を作らない
ため、`Entropy` は std::tls が引く乱数を **全部** 賄うことを設計の条件にする。

現在の消費箇所(`lib/kizu/std/src/` で `std::crypto::random_bytes` / `random_into`、
その下の builtin `crypto_random_into` を呼ぶ所を全部見た結果):

| # | 箇所 | 量 | 用途 |
| --- | --- | --- | --- |
| 1 | `tls/client.kizu:38` `handshake` | 96 byte | ClientHello.random 32、legacy_session_id 32、x25519 秘密鍵 32 |
| 2 | `tls/server.kizu:112` ClientHello 受信後 | 64 byte | ServerHello.random 32、x25519 秘密鍵 32 |
| 3 | `tls/server.kizu:284` `send_certificate_verify` | scheme の digest 長(32 / 48) | RSA-PSS の salt。EC 鍵でも引く(`x509::sign` は RSA でしか読まない) |

std::tls と std::http の中で乱数を引くのはこの 3 箇所だけで、OS の乱数を直接引く
フォールバックは無い。消費箇所が無いことを確かめたもの: ECDSA 署名(RFC 6979)、
x509 の検証、HelloRetryRequest(client は受けたら `handshake_failure` で閉じ、key share を
引き直さない。server は送らない)、session ticket(発行しない。受けたものは読み捨てる)、
resumption / PSK / 0-RTT(無い)、KeyUpdate(相手への応答だけで乱数は要らない)、
record の padding(付けない)、alert、http client / server(boundary や nonce を作らない)。

設計の条件:

- PR A で 3 箇所とも `Connection` が持つ `Entropy` から引く形に替え、tls / http の中に
  `std::crypto::random_bytes` を直接呼ぶ所を残さない。OS の乱数に届くのは
  `system_entropy` の実装 1 箇所だけになる。
- 今後 std::tls に乱数の消費(ticket の発行、resumption、HRR の送信、padding など)を
  足すときも同じ `Entropy` から引く。`Entropy` を差したのに OS の乱数を引く経路は、
  動いていても設計上の欠陥として扱う。
- 引く量と順序は今と変えない(上の 3 箇所を上から順)。salt を EC 鍵で引かないように
  するのは wire に効かないが、PR A は振る舞いを変えない PR なので別の判断にする。

確かめ方:

- PR A の behavior test で、同じ seed の `predictable_entropy` を差した接続を同じ process の
  中で 2 回張り、両方向の全 record が byte で一致することを見る。相手も Kizu にして
  相手側も同じ seed にすれば、どちらかに OS の乱数が 1 byte でも混ざると 2 回目が違う。
  server は P-256 と RSA の鍵の両方で張る(#3 は鍵の種類で読まれ方が違う)。
- PR B の再生は、fixture と byte で一致した時点で同じことを全 case について示す。
  さらに各 fixture を 2 回続けて再生し、2 回とも一致することを見る。
- grep で「`random_bytes` を呼ぶ所が無い」ことを固定する構造 pin は足さない
  (AGENTS.md)。上の実行結果で検出する。

## 4. 再生

### 4.1 共通の仕組み

新しい behavior module `tests/behavior/src/tls_replay/tls_replay_test.kizu` に置く。
1 package で link も実行も 1 回なので、case ごとに `kizu run` する今より速く、
`Selfhost` の Behavior で両 compiler の出力が同じ再生を通る。

- 1 つの test が `io::loop_new` と `io::evented` で loop を作り、`127.0.0.1:0` で listen し、
  再生役を `io::spawn` の worker、Kizu 側をもう片方に置く(`http_server_test.kizu:1697`
  の形)。fixture は `fs::read_file` で読む。
- 再生役は trace を先頭から 1 行ずつ進める。`<` の行はその bytes を書く。`>` の行は
  record header を読んで 1 record 分を読み、録ったものと比べる。録った順は Kizu から
  見て因果順(§2.2)なので待ち合わせで詰まらない。最後の行の後は相手側から閉じ、
  Kizu が閉じるまで読んで余分な bytes が無いことを確かめる。
- 比べ方: byte 単位で一致すること。例外は `# port:` を持つ fixture(http client)の
  application data だけで、録った record と受け取った record をそれぞれ keylog の
  `CLIENT_TRAFFIC_SECRET_0` から `tls::traffic_key` で作った鍵と `tls::open_record` で
  開き、録った平文の port を今回の port に置き換えてから平文を比べる。鍵の導出と
  record の開封は RFC 8448 で検証済みの関数(`tests/behavior/src/tls/tls_test.kizu`)、
  secret は node が出したもの。
- 一致しないときは fixture 名、行番号、向き、注釈、両方の先頭 32 byte を出し、
  「意図した変更なら `just tls-fixtures` で録り直す」と添える。
- 実装の最初に確かめること: worker の中で `testing` の判定が使えるか。使えなければ
  worker は結果(一致した行数と、不一致の説明)を自分の state に書いて返し、test 側で
  判定する。

### 4.2 テストごと

| 現行 case | 再生する fixture | 一致・成功の判定 |
| --- | --- | --- |
| tls_client echo / P-384 / RSA | `client-echo-*` | Kizu の record が全部一致。`cipher_suite()` が AES-128-GCM、読んだ返事が `HELLO OVER TLS`、`close` が通る |
| tls_client wrong host | `client-wrong-host` | `connect_with` が `NameMismatch`。送った alert record(handshake 鍵で暗号化)が一致 |
| tls_client wrong root | `client-wrong-root` | `UnknownIssuer`。alert record が一致 |
| tls_server 256 / 384 / 2048 | `server-echo-*` | 一致。`server_name()` が `localhost`、suite、読んだ行が `hello over tls`、`close` が通る |
| tls_server wrong root | `server-wrong-root` | Kizu の flight が一致し、`accept_wait` が `Alert` で終わる(相手の bytes が固定なので `ConnectionReset` の揺れは無くなる) |
| tls_server Kizu client | なし | 両端 Kizu のまま残す(§6)。`system_entropy` で接続し出力を確かめる |
| http fetch | `https-client-fetch` | handshake 部が一致、request の平文が port 置換で一致、status `200` と本文 `hello over tls from /greeting` |
| http wrong root / wrong host | `https-client-wrong-*` | `UnknownIssuer` / `NameMismatch`。alert record が一致 |
| http no roots | なし | `fetch_with` が接続前に `NoTrustedRoots`(今と同じ) |
| http server accept / loop | `https-server-keepalive` → `-fresh` → `-stranger` を順に | 3 本とも一致し、server が `served /one` `/two` `/three` を出す。stranger は handshake で拒まれ、server は次へ進む。accept と loop が同じ fixture に一致する(同じ wire を出すことの検査にもなる) |

case は 16 のまま全部残る。http client の fetch / wrong root / wrong host は
`fetch_with` から `connect_secure_with` + `send` / `receive` に替わる(§6)。

## 5. 実装の分け方

### PR A: 乱数の口

- `std::tls::Entropy` と §3.2 の関数、`http` の `_with` 2 つ。既存関数は `_with` を呼ぶ形に。
- `docs/std/tls.md` と `docs/std/http.md` に API を足す。
- 乱数の消費 3 箇所(§3.3)を全部 `Entropy` から引く形にする。
- behavior test: 同じ seed なら ClientHello と server の flight が同じ bytes、違う seed なら
  違う、`split` の子どうしが違う。同じ seed で 2 回張った接続の全 record が一致する(§3.3)。
- 完了条件: 既存の Go の TLS テスト 3 本がそのまま通る(既定の振る舞いが変わらない)、
  `just spec-table` の生成物が変わらない(状態機械は触らない)、`go test ./...` と
  `just selfhost`、`pre-commit run --all-files`。

### PR B: fixture と再生、Go のテストの削除

最初に確かめること: 有効期間 9999-12-31 の証明書(§2.3)を Kizu 自前の x509 が受け入れる
こと。`der.kizu` の `read_time` / `time_value` は GeneralizedTime(4 桁の年)を読む形を
持つが、year 9999 の `notAfter` を実際に通した test はまだ無い。作った証明書を
`std::crypto::x509` で読み、`notAfter` が 9999-12-31T23:59:59Z の秒になることと、
実時計で検証が通ることを behavior test で確かめる。通らなければ、parser を直すか
有効期間を縮めるかを決める論点として、先へ進まずに止めて報告する。

commit を分ける: 証明書と鍵 → 録る道具と just → fixture 15 本 → 再生 module → Go の
テスト 3 本と helper(`selfSignedServer` / `generateKey` / `serveOnce` /
`startKizuServer` / `kizuServer` / `mustParse`)の削除。

完了条件:

- §1.2 の 16 case が §4.2 の対応で全部あり、期待する結果が同じ。
- わざと壊すと落ちる: ClientHello の拡張の順を入れ替える、server の alert を別の種類に
  する、`write` の分割を変える、fixture の 1 byte を変える(sha256)、相手の record の
  1 byte を変えて sha256 を直す(Kizu が `BadRecord`)、trace の最後の行を消す。
  それぞれ落ちることを PR に書く。
- 所要時間: `go test ./cmd/kizu` は 3 本分の `kizu run` が消えて短くなる。behavior の
  増分を前後で測って PR に書く(目安 1 秒以内)。
- `git grep -l '"crypto/tls"\|"crypto/x509"\|"net/http"' -- '*.go'` が空になる
  (現在この 3 本以外に使っている Go のファイルは無い)。

## 6. 決めること

| # | 問い | 推奨 | 理由 |
| --- | --- | --- | --- |
| 1 | 乱数の口の形 | 案1b(`Entropy` を接続に持たせ、`_with` を足す) | 既存の呼び出しが変わらず、http server の worker まで届く唯一の案。Io に持たせる案4 は危険の範囲が program 全体になる |
| 2 | 予測できる源の名前 | `predictable_entropy` | grep で再生用の箇所を列挙できる 1 語(原理 3)。`test_` を付けても std の外で使える点は同じなので、危険そのものを名前にする |
| 3 | 再生の場所 | `tests/behavior` の Kizu module | PR #1996 の `crypto_vectors` / `x509_vectors` と同じ置き方。両 compiler で回り、1 回の link で済む。Go のテストを byte の中継役として残す案は Go への依存が残り、case ごとの `kizu run` も残る |
| 4 | 録る相手 | devShell の node | 既に devShell にあり version が固定。openssl を足すより devShell が増えない |
| 5 | http client の case を `connect_secure_with` で書くこと | 書き換える | `fetch_with` に `_with` を足すと引数がさらに増える。URL から https を選ぶ部分は `no roots` の case と `http_client` の behavior が見ている |
| 6 | 両端 Kizu の case(tls_server の「Kizu client」) | 残す(fixture なし、`system_entropy`) | 比較相手は自分だが、OS の乱数で通る経路を見る唯一の case で、安い |
| 7 | 証明書の有効期間を 9999-12-31 まで取ること | 取る | http client が実時計を読むので、期限で fixture が腐らないようにする。期限切れの判定は `x509_vectors` が見ている |

7 は Kizu の x509 が year 9999 の GeneralizedTime を扱えることが前提で、PR B の最初に
確かめる(§5)。扱えなければそこで止めて報告する。
