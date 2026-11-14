# WebSocket(RFC 6455)設計: issue #1982 の「決めること」への推奨案

issue #1982 が挙げた 8 つの問いに、推奨・根拠・採らない案を 1 つずつ書きます。
前提(101 の後の所有は ADR-0138 で決まっている)、対象外、検証の方法(codec は
Quint モデル + モデルベーステスト、SHA-1 / base64 は test vector + 差分テスト)は
issue のとおりとし、ここでは繰り返しません。

調査時点は origin/main 4bad961 です。他言語の記述は次の一次情報で確かめました。

- gorilla/websocket: `doc.go` / `conn.go` / `server.go`(main)
  https://github.com/gorilla/websocket
- coder/websocket: README と `read.go` / `write.go` / `close.go` / `accept.go`(master)
  https://github.com/coder/websocket
- golang.org/x/net/websocket: `websocket.go` / `hybi.go`
  https://github.com/golang/net/tree/master/websocket
- Zig std: `lib/std/crypto/Sha1.zig`、`lib/std/base64.zig`、`lib/std/http/Server.zig`(0.16.0 と master)
  https://codeberg.org/ziglang/zig
- karlseguin/websocket.zig: README
  https://github.com/karlseguin/websocket.zig

## 全体像

推奨を 1 枚にすると次の形です。各節はこの形のどこを決めたかを書きます。

```text
std::websocket
  Session            codec の状態。接続を持たない(sans-IO)。受信 buffer と
                     送信待ちの bytes(outbox)を持つ owner
  advance(state, event) -> Transition
                     frame の種類ごとの状態遷移。std::tls::advance と同じ形で、
                     std::machine が表を読み、Quint モデルと MBT がそこから来る

  // I/O は接続を呼び出しごとに借りる薄い関数。C は http::Exchange か http::Connection
  upgrade(io, allocator, exchange: &var http::Exchange, origin, protocols) -> Failure!Session
  open(io, allocator, conn: &var http::Connection, host, target, protocols) -> Failure!Session
  read<C>(io, allocator, conn: &var C, session: &var Session, out: &var String, max) -> Failure!Received
  read_ready<C>(...)  -> Failure!?Received
  session.queue_text / queue_binary / queue_ping / queue_close(...)   // outbox に積むだけ
  flush<C>(io, allocator, conn, session)        -> Failure!void   // write_all
  flush_ready<C>(io, allocator, conn, session)  -> Failure!i64    // write_some。残りは outbox
```

要点は 3 つです。

1. **read は書かない。** 書くのは `flush` / `flush_ready` だけで、caller の source に
   見えます(原理 2)。ping への pong、close の echo、protocol error の close は
   read が outbox に**積み**、caller の次の flush で出ます。
2. **outbox が唯一の書き手。** data も制御も同じ outbox に frame 単位で積むので、
   `write_some` で frame の途中まで書いた状態でも、pong が frame の中に割り込む
   ことがありません。8 の並行規則はこれで決まります。
3. **上限は read の引数。** 1 message の最大を呼び出しごとに渡します(ADR-0143)。

## 1. SHA-1 と base64 の置き場所

### 推奨

- **SHA-1 は `std::crypto::sha1` として public にする(案 A)。** 形は `sha256` と
  同じ one-shot + caller buffer(`sha1(bytes, digest: &var []u8) -> void`)。
  `Hash` enum には**足さない**。doc に「衝突耐性が破れている。protocol identifier
  (WebSocket の accept key など)にだけ使う」と書く。
- **base64 は新しい public module `std::base64` に encode / decode を置き、PEM を
  それに切り替える(案 A)。** 第 1 弾の alphabet は standard + padding 必須のみ。
  URL-safe と padding 無しは後から足す(追加で済む)。
- 同じ理由で、**UTF-8 検証も `std::string` の public な関数にする**
  (`docs/std/string.md` が未実装に挙げている箇所を埋める)。

### 根拠

- **std の test は public API 越しにしか書けない。** std の source には test block が
  無く、`std::crypto` の test は `tests/behavior/src/crypto/crypto_test.kizu` が
  public 関数を呼んでいます。issue が決めた検証 —— FIPS 180-4 / RFC 4648 の vector の
  網羅(空、55 / 56 / 64 byte 境界、複数 block、長大入力)と、既存実装を oracle に
  したランダム差分 —— は、関数に test から届かないと書けません。private にすると
  届くのは accept key 経由の 1 本だけで、検証の約束を満たせません。
- 原理 8(迷ったら閉じる)とはここでぶつかりますが、迷いではなく**検証の要求で
  開く**ので、原理 8 の「実需が現れてから開く」の条件を満たします。
- **Go も Zig も SHA-1 を他の hash と同じ場所に置き、警告を doc に書く。**
  Zig は `std.crypto.hash.Sha1` で、`Sha1.zig` の冒頭に "The SHA-1 function is now
  considered cryptographically broken." と書いています。Go は `crypto/sha1`。
  WebSocket 側に閉じ込めている例は見つかりませんでした。
- **`Hash` に足さないのは原理 7。** `Hash` は署名検証が「どの hash で検証するか」を
  受け取る名前で、そこに SHA-1 があれば SHA-1 署名を受け入れる道が開きます。
  識別子としての SHA-1 と、署名に使う hash は契約が違います。
- **base64 の decoder を 1 つにするのは原理 9。** `crypto/x509/pem.kizu` の
  `base64_value` と新しい decoder が並ぶと、同じ文法を 2 つが解釈します。
  Zig も `std.base64` 1 つで、PEM 系もそれを使います。
- alphabet を standard に絞るのは、今の 2 つの用途(accept key と PEM)がどちらも
  standard + padding だからです。Zig は `standard` / `standard_no_pad` / `url_safe` /
  `url_safe_no_pad` の 4 つを持ちますが、Kizu に足すのは需要が出てからで足ります。
- SHA-1 の実装は `sha256_parts` と同じ「複数の view を連結せずに hash する」形にでき、
  accept key の `key ++ GUID` も連結の確保なしに計算できます。

### 採らない案

| 案 | 理由 |
| --- | --- |
| SHA-1 を WebSocket のコードの中で private にする(1-B) | test vector と差分テストが書けない(std の test は public API 越し)。test のためだけの公開経路を足すと原理 9 の「経路は 1 本」に反する |
| std の source に test block を持ち込んで private を検査する | std の test を `tests/behavior` に集めている今の構成を、この 2 関数のためだけに崩す |
| base64 encoder を WebSocket 内に private、PEM は自分の decoder のまま(1-B) | private な base64 部品が 2 つになり、片方だけ直る状態を作る。検証も上と同じ理由で書けない |
| `std::base64` に URL-safe も最初から持つ | 用途が無い。後から追加できる(原理 8) |
| UTF-8 検証を codec の中で private にする | 1007 の判定に使う関数で、境界(overlong、surrogate、4 byte の上限)の vector を直接当てたい。`docs/std/string.md` がすでに欠落として挙げている |

## 2. モジュールと所有権

### 推奨

- **所有は動かさない(案 A)+ codec は sans-IO(案 C)。** `Session` は接続を
  持たない値で、I/O 関数は呼び出しごとに `&var Exchange` / `&var Connection` を
  借ります。caller は「接続」と「Session」の 2 つを持ちます。
- **I/O 関数は contract で Exchange と Connection の両方を受ける 1 本にする。**
  両者はすでに `read_into` / `read_ready_into` / `write_all` を同じ signature で
  持っているので、`read<C>` / `flush<C>` を static generic にすれば codec も
  wrapper も 1 つで済みます。
- **module は top-level の `std::websocket`。** handshake だけが `std::http` に
  依存し、codec は HTTP を知りません。

### 根拠

- **ADR-0138「所有は変わりません」に文字どおり合う。** ADR-0138 は `into_stream`
  を「所有権を手放す必要が実際には無い —— `Exchange` が生きている間 raw に
  読み書きできれば WebSocket の loop は書ける」で却下しています。
- **案 B(move)の利点が Kizu では消える。** B の動機は「TaskSet worker に渡す
  owner が 1 つで済む」ですが、worker はすでに `Exchange` を受け取ってから
  upgrade するので(`docs/std/http.md`「接続ごとの worker」)、`Session` は worker の
  frame で作られ、move する必要がありません。poller loop の接続表も、要素の struct に
  `Exchange` と `Session` を並べるだけです。
- **Kizu は struct field に borrow を持てない**(`docs/language-gaps.md`)。
  `Socket { exchange: &var Exchange }` は書けないので、A の選択肢は「呼び出しごとに
  借りる」だけです。これは http の `Exchange` がすでに選んだ形です。
- **sans-IO にすると検証が状態機械 1 つに集まる。** `std::tls::advance(state, event)`
  が前例で、`std::machine` がその遷移関数から表を読み、`spec/` の Quint / Lean が
  そこから来ます。WebSocket の codec も `advance` を public な純関数にすれば、issue が
  決めた「Quint モデル → MBT」がそのまま同じ道具に載ります。blocking の `read` と
  poller の `read_ready` は同じ `advance` を呼ぶので、経路は 1 本です。
- **top-level にするのは `std::tls` と同じ理由。** tls は http に使われる別の protocol
  で、http の下にはありません。WebSocket は HTTP で始まるだけの別の protocol で、
  error も別です(4 を参照)。RFC 8441(HTTP/2 上)が来ても codec は変わらず、
  handshake 関数が増えるだけです。

### 採らない案

| 案 | 理由 |
| --- | --- |
| B. `accept(move exchange) -> Socket` が Exchange を所有する | Kizu では得るものが無い(上記)。加えて `Socket.deinit` に close handshake を持たせたくなるが、`deinit` は失敗できず、書き込みを隠すことになる(4 を参照)。client 側は `Connection` も所有する別の型が要り、型が 2 つになる |
| A を codec 無しで書く(read が直接 bytes を読んで解釈する) | blocking と poller で解釈を 2 回書くことになる。状態遷移が関数として取り出せず、Quint モデルとの対応が取れない |
| server 用・client 用に I/O 関数を 2 組書く(`read_server` / `read_client`) | 公開面が倍になり、片方だけ直る |
| `std::http::websocket`(submodule) | codec は HTTP に依存しない。error set も HTTP と別(原理 7)。x509 が `std::crypto` の下にあるのは「暗号の data」だからで、WebSocket は HTTP の data ではない |

## 3. caller が見る単位: message か frame か

### 推奨

**read が返すのは message 単位**(`Text` / `Binary`)と、制御フレーム(`Ping` /
`Pong` / `Close`)です。fragment の再構成は read が caller の上限まで行います。
frame 単位の stream API は第 1 弾では公開しません。frame の区別が見えるのは
`advance` の event(検証用の純関数)だけです。

```kizu
let got = try websocket::read<http::Exchange>(handle, allocator, &var exchange, &var session, &var message, 65536);
match got.kind {
    Text => ...,      // message に本文が入っている(UTF-8 検証済み)
    Binary => ...,
    Ping => {},       // pong は outbox に積まれている。loop の flush で出る
    Pong => {},
    Close => ...,     // got.code、理由は message に。echo は outbox に積まれている
}
```

### 根拠

- **動機の用途(ブラウザ UI の更新)は小さい message の往復**で、caller が
  fragment を気にする理由がありません。frame 単位を見せると再構成がすべての
  caller に任され、原理 10(定型が call site ごとに増える)に当たります。
- **Go の両実装とも frame を公開していない。** gorilla は `ReadMessage` /
  `NextReader`、coder は `Read` / `Reader` で、どちらも message 単位です。大きな
  message は message 単位の `io.Reader` で stream します。
- **x/net/websocket は反面教師。** `Conn.Read` は「frame のデータを読み、buffer が
  足りなければ次の Read が残りを読む」で、`net.Conn` / `io.Reader` として見せた
  結果 message の境界が失われました。パッケージ doc 自身が gorilla と coder を
  推しています(golang/go#18152 は open のまま)。
- **Zig std の `std.http.WebSocket` は fragment を扱わない。** `readSmallMessage` は
  FIN の無い frame を `MessageOversize` で拒否します。build system の web UI だけが
  使う最小実装で、std の汎用部品としては範囲が狭すぎます。Kizu は再構成まで持ちます。
- stream API を後から足すのは追加で済みます(原理 8)。codec は内部で frame 単位に
  動いているので、足すときは同じ `advance` の上に別の read を置くだけです。

### 採らない案

| 案 | 理由 |
| --- | --- |
| frame 単位だけを公開する | 再構成と「制御フレームが間に挟まる」処理が全 caller に行く(原理 10) |
| message と frame の両方を第 1 弾で公開する | frame 版の需要が無い。開→閉は breaking なので、需要が出てから開く(原理 8) |
| `net.Conn` 風の byte stream として見せる | x/net/websocket の失敗そのもの。message 境界が消える |

## 4. 制御フレーム(ping / pong / close)

### 推奨

**read は書かない。制御フレームを caller に返し、protocol が要求する応答は
read が outbox に積む。** 出すのは caller の `flush`。

- **ping:** read は pong を outbox に積み、`Ping` を返す。caller は loop で flush する。
- **pong:** `Pong` を返すだけ。
- **peer の close:** close を echo する frame(同じ code)を outbox に積み、`Close` を
  返す(`code` と、理由は `out` に)。以後 session は data を積めない。
- **自分から close:** `session.queue_close(allocator, code, reason)` + `flush`。
  peer の close を待つのは caller の次の `read` で、待つ時間は caller の
  `set_read_deadline` が持つ。`linger_millis` のような budget を codec は持たない。
  close handshake が済んだら server は `Exchange.deinit` で先に TCP を閉じ
  (RFC 6455 §7.1.1)、client は `read_into` の 0 まで読む。
- **`Session.deinit` は memory を解放するだけで書かない。** close を交換しないまま
  `Exchange.deinit` すれば peer には 1006(異常終了)として見え、それが事実です。
- **protocol error / 上限超過:** read は名前の付いた error を返し、session は対応する
  close(1002 / 1007 / 1009)を outbox に積んで `Failed` に移る。caller は flush して
  (1 回、待たない)閉じる。RFC 6455 §7.1.7 はこの close を SHOULD としている。
- **受け取った close code の検査:** 1005 / 1006 / 1015 と未割り当ての範囲は 1002。
  code の無い close は `code` を 1005 として返す(wire には書かない)。理由が不正な
  UTF-8 なら 1007。

### 根拠

- **原理 2: すべての write が caller の source にある。** 書くのは `flush` /
  `flush_ready` だけで、read の内部に書き込みはありません。
- **read が書くと 8 の並行規則が壊れる。** poller loop は `write_some` で frame を
  途中まで書いた状態を持ちます。そこで read が pong を直接書くと、pong が書きかけの
  frame の中に入り、stream が壊れます。outbox に積めば、pong は frame の境界に入ります。
- **案 B(caller が pong を書く)と比べて定型が増えない。** B だとすべての loop に
  `Ping(p) => try websocket::pong(...)` が要り、`_ => {}` で落とせば ping は無視
  されたまま気づかれません(原理 10)。推奨では「flush を忘れた」は自分の message も
  出ないので、最初の実行で気づきます。
- **案 A(read の中で書く)の前例は弱い。** `accept` が 400 を書くのは「caller は parse
  すらしていない request に status を送れない」ためでした(ADR-0136)。WebSocket の
  caller は flush を呼べるので、この理由が当てはまりません。
- **read は制御フレームでも返る必要がある。** blocking の read が ping を消費して次の
  data を待ち続けると、積んだ pong が flush されないまま止まります。だから
  `Ping` / `Pong` も返します。
- **Go / Zig の比較。** gorilla は既定の ping handler が read の中で pong を書き、close
  handler が close を書きます("can block these methods for a short time when the
  handler writes to the connection")。coder も Reader が制御フレームを処理し、Close は
  書いてから 5 秒待ちます。どちらも「read の中で書く」を選び、その代わり書き込みに
  mutex を持っています。Kizu は thread を持たない loop が前提なので、mutex ではなく
  outbox で境界を守ります。Zig std の `std.http.WebSocket` は ping を caller に返し
  自動 pong はしません(close も echo しない)。websocket.zig は callback を定義しなければ
  自動で pong / close を返す framework 型です。推奨は「caller に返す」点で Zig std に、
  「応答を用意する」点で Go に近く、書くのは caller です。
- **close の待ち時間を codec が持たないのは ADR-0137 / ADR-0138。** deadline は時点で、
  socket の owner が持ちます。coder の「5 秒」は library が決めた policy で、Kizu は
  policy を caller に置きます(ADR-0136 の却下表、connection pool と同じ理由)。
- **`deinit` が書かないのは、失敗を返せない関数に I/O を持たせないため。** http の
  `Exchange.deinit` も閉じるだけです。

### 採らない案

| 案 | 理由 |
| --- | --- |
| A. read の中で pong / close を書く | 書き込みが read の中に隠れる(原理 2)。`write_some` の書きかけ frame に割り込む |
| B. 制御フレームを返し、応答は caller が書く | 全 loop に同じ arm が増え(原理 10)、落とせば黙って protocol 違反になる。結局 poller では pong を outbox に入れる仕組みが要る |
| `Session.deinit` / `Exchange.deinit` で close frame を送る | `deinit` は失敗を返せない。書き込みを `defer` の中に隠す |
| close 後に peer を待つ budget(`linger_millis` 相当)を codec に持たせる | codec は I/O を持たない。待つのは caller の read で、期限は caller の deadline |
| 自動の keep-alive ping | issue で対象外。時間は loop が持つ(`expired(now)` が前例) |

## 5. 上限

### 推奨

- **上限は 1 つの数: 再構成後の 1 message の最大 byte 数を、read の引数で渡す**
  (`read(..., out, max: i64)`)。`Limits` struct は作らない。
- frame の上限は別に持たない。frame header の長さを読んだ時点で「この message の
  残り」と比べ、超えれば payload を 1 byte も読まずに拒否する。64 bit 長の最上位
  bit が立っていれば protocol error。制御フレームは protocol が 125 byte に固定する。
- **超えたら `websocket::Error::MessageTooBig`** を返し、1009 の close を outbox に積む。
  `OutOfMemory` とも protocol error とも別の名前(原理 7)。
- **`Unlimited` は持たない。** 型は `mem::Limit` ではなく `i64`(`accept` / `read_into` の
  `max` と同じ)。
- **deadline は今のまま caller のもの。** upgrade 後の loop は `set_read_deadline` を
  自分で押し直す。codec は idle budget を受け取らない。

### 根拠

- **ADR-0143「上限は byte を求める呼び出しの引数」。** body の上限を `Limits` から
  外し、`accept(handle, allocator, 65536)` に移した理由 —— endpoint ごとに受け取るものが
  違う —— は WebSocket の message にもそのまま当てはまります。Zig の
  `readAllAlloc(allocator, max)` も「max を渡さないと呼べない」形です。
- **数を 1 つにするのは、frame の上限が message の上限から導けるから。** frame は
  message の一部なので、「message の残り」を超える frame は必ず拒否されます。
  受信 buffer の大きさは `max` + header(14 byte)+ 制御フレーム(125 byte)で
  抑えられます。
- **既定の上限が無い危険を、引数の必須化で消す。** gorilla は `SetReadLimit` の既定が
  0(無制限)で、呼び忘れると 8 EiB を名乗る header を信じます。coder は既定を
  32768 にし、`-1` で無効にします。Kizu は既定を持たず、呼び出しに数を書かせます。
  これは原理 3 の「危険を型で禁止できないとき」より強く、型(必須引数)で閉じられる
  ので閉じます。上限なしで受けたい caller が現れたら、その時に別の関数を足します。
- **超過を別の error にするのは原理 7。** 入力を拒否して回復するもの(超過)と、確保に
  失敗したもの(OOM)は違います。gorilla の `ErrReadLimit`、coder の
  `ErrMessageTooBig` も専用の値です。
- **deadline を codec に入れないのは ADR-0137 / ADR-0138。** 「`write_all` と `read_into`
  は deadline に触らない。1 phase より長く生きる caller は自分で押し直す」がそのまま
  使えます。codec は sans-IO なので、そもそも時間を測れません。

### 採らない案

| 案 | 理由 |
| --- | --- |
| `upgrade` で 1 回渡す `Limits` struct | 呼び出しを読んでも上限が見えない。ADR-0143 が `max_body_bytes` を `Limits` から外した理由と同じ |
| frame と message の上限を別々に渡す | frame の上限は message の上限から導ける。数が 2 つあると「frame は通るが message で落ちる」組み合わせの説明が増える |
| `std::mem::Limit` を受けて `Unlimited` を許す | 64 bit 長の header 1 つで任意の確保を要求できる。必須の数で閉じられるものを開けない(原理 8) |
| 超過を `OutOfMemory` か protocol error に畳む | 原理 7 |
| codec が idle budget を受け取る | codec は I/O を持たない。deadline は socket の owner のもの |

## 6. 第 1 弾の範囲: server、client、両方

### 推奨

**server と client の両方を第 1 弾に入れる。** codec は 1 つで、role(server /
client)が mask の向きを決めます。

- client の書き込みは frame ごとに 4 byte の mask key が要る。codec(`queue_*`)は
  純関数のまま、**mask key を引数で受け取る**。I/O 側の client 用 flush が
  `std::crypto::random_into` で key を取る。
- handshake の 16 byte の nonce も同じく `random_into` から取る。
- server は mask されていない client frame を、client は mask された server frame を
  protocol error(1002)にする。

### 根拠

- **検証が両端を要求する。** issue の検証(MBT、behavior test、example)は 1 プロセスで
  両端を動かす形(`examples/http_upgrade.kizu`)で、test にブラウザはいません。
  server だけにすると、test 用の masking writer が要ります。
- **test 専用の経路は原理 9 に反する。** 「test 用に最小の masking writer を持つ」案は、
  利用者が通らない 2 つ目の書き手を std に置くことになります。どうせ書くなら、
  利用者が通る client として書く方が経路は 1 本です。
- **差分が小さい。** RFC 6455 の server と client の違いは mask の向きと handshake の
  向きだけで、frame の文法も状態機械も同じです。mask key を引数にすれば、codec は
  乱数源を持たず、MBT は決まった key で再現できます。
- **Go の 2 実装はどちらも client を持つ**(gorilla の `Dialer`、coder の `Dial`)。
  gorilla は mask key を frame ごとに `crypto/rand` から取ります。Zig std の
  WebSocket は server のみですが、build system の web UI(相手はブラウザ)専用です。
- Kizu 同士の通信(UI ではない双方向の用途)もこれで書けます。

### 採らない案

| 案 | 理由 |
| --- | --- |
| server のみ | test と example に client が要る。ブラウザを test に入れられない |
| server のみ + test 用の最小 masking writer | 利用者が通らない書き手が std に残る(原理 9) |
| codec が乱数を取る(`Io` を codec に渡す) | codec が sans-IO でなくなり、MBT が乱数を固定できない |

## 7. handshake の表面

### 推奨

**server: `websocket::upgrade(io, allocator, &var exchange, origin, protocols) -> Failure!Session`**

- `accept_head` の後、caller が path で振り分けてから呼ぶ。caller が先に set した
  response header(`Set-Cookie` など)はそのまま 101 に乗る。
- 検査: `GET`、HTTP/1.1、`Upgrade` に `websocket`、`Connection` に `upgrade`
  (token list、大文字小文字を区別しない)、`Sec-WebSocket-Version: 13`、
  `Sec-WebSocket-Key` が 16 byte の base64。
- **失敗したら std が status を 1 回だけ、待たずに書いて error を返す。** 400
  (形が違う)、403(origin)、426 + `Sec-WebSocket-Version: 13`(version 違い)。
  `accept` の拒否と同じ規則(`docs/std/http.md`「読めなかった request」)。
- 成功したら `respond_head(Raw)` で 101 を書き、`Upgrade` / `Connection` /
  `Sec-WebSocket-Accept` /(選んだら)`Sec-WebSocket-Protocol` を付け、`Session` を返す。
- **`origin` は必須の引数**で、空の既定は無い。`SameHost`(`Origin` の host が `Host` と
  一致)、許可 list、`AnyOrigin`(grep できる危険語)の 3 つ。`Origin` header の無い
  request(ブラウザ以外)は通す。
- **subprotocol は server が対応する list を渡し、std が client の提示と照合して
  1 つ選ぶ**(server 側の優先順)。空の list なら選ばない。
- **extension は negotiate しない。** `Sec-WebSocket-Extensions` は無視し、応答に
  書かない。

**client: `websocket::open(io, allocator, &var conn, host, target, protocols) -> Failure!Session`**

- `http::connect` / `connect_secure` の直後の `Connection` に、upgrade request を
  `write_all` で書き(`send_with` は `Connection: close` を書くので使わない)、
  `receive` で 101 を読む。
- 検査: status 101、`Upgrade` / `Connection`、`Sec-WebSocket-Accept` が自分の nonce から
  計算した値と一致、選ばれた protocol が自分の提示に含まれる、extension が無い。
  101 以外は status を持つ error。

token list の helper は `std::websocket` の中で private に持ちます(使うのは
handshake だけ)。

### 根拠

- **検証を std が持つのは原理 10。** 検査項目は RFC が固定した 6 つで、caller ごとに
  書き分ける情報がありません。失敗の status(400 / 426 + header)も RFC が決めて
  います。`upgrade` 1 回の呼び出しが source に見えているので、隠れた制御フローでは
  ありません。
- **拒否を std が書く前例は `accept` と `refuse_expired`。** どちらも「1 回だけ、
  待たない」で書きます(ADR-0141: 読まない peer のために待つと loop 全体が止まる)。
  gorilla(`Upgrade` は失敗時に HTTP error を書く)、coder(「Accept will write a
  response to w on all errors」)も同じ分担です。
- **origin を必須にするのは、ブラウザが WebSocket に CORS を掛けないから。** 検査を
  忘れた server は cross-site WebSocket hijacking をそのまま受けます。Go の 2 実装は
  どちらも**既定で cross-origin を拒否**します(gorilla: `CheckOrigin` が nil なら
  「Origin が在り、その host が Host と違えば拒否」、coder: 「Accept will not allow
  cross origin requests by default」、無効化は `InsecureSkipVerify`)。Kizu は既定値を
  持たない代わりに引数を必須にし、無効化を `AnyOrigin` という 1 語にします(原理 3)。
  coder の doc が `"*"` pattern ではなく `InsecureSkipVerify` を使えと言うのも、
  危険を名前で見せる同じ考えです。
- **`Origin` の無い request を通すのは両 Go 実装と同じ**で、ブラウザ以外の client は
  Origin を自由に偽れるので、検査しても守るものがありません。
- **subprotocol を list で渡すのは gorilla の `Subprotocols`(server の優先順で照合)と
  同じ形。** caller が `Sec-WebSocket-Protocol` を token list として parse する必要が
  無くなり、token list の helper を公開せずに済みます(原理 8)。
- **Zig std は検査をほぼしない**(`upgradeRequested` が key を返し、
  `respondWebSocket` が 101 を書くだけで、origin も subprotocol も扱わない)。
  build system の localhost 用 UI には足りても、公開 server の std には足りません。

### 採らない案

| 案 | 理由 |
| --- | --- |
| 検査結果を caller に返し、status は caller が書く | 全 caller が同じ 400 / 426 を書き分ける(原理 10)。426 に付ける header を忘れる道が開く |
| origin の検査を caller に任せる | 忘れた server が既定で穴を持つ。Go の 2 実装が既定で塞いでいるものを、Kizu だけが開ける |
| `SameHost` を既定値にして引数を省ける | 既定を持つ引数は Kizu に無い。必須の enum 1 つで、読む人に方針が見える |
| caller が提示された subprotocol から自分で 1 つ選んで渡す | token list の parse を caller に出すことになり、helper の公開が要る。選び方は「対応 list の優先順」で十分 |
| `Upgrader` のような設定 struct を作る | 1 回の呼び出しの引数で足りる。設定を値に持つ理由が無い |

## 8. 受信と push の並行

### 推奨

- **第 1 弾の example は poller loop**(`watch_read` + `read_ready` + deadline 付きの
  `Poller.wait`)で、1 thread が client の message を待ちながら push する形を示す。
- **`Exchange` と `Connection` に `write_some` を public で足す。** 中の `Reader` /
  `Stream` にはもうあり(`lib/kizu/std/src/http/stream.kizu`)、pass-through を 1 つ
  開けるだけです。`flush_ready` がそれを使い、書けなかった残りは outbox に残します。
- **書き込みの規則:** 接続 1 本に書くのは `Session` の outbox だけ。data も制御も
  outbox に frame 単位で積み、frame の境界でしか混ざらない。read は書かない(4)。
  同じ `Session` を 2 つの task から触ることは、所有規則がそもそも許しません。
- TaskSet worker の形(blocking read)は「受けて返す」だけの用途(echo、
  request / response 型)として doc に書き、push との両立は task をまたぐ channel が
  入ってから(issue のとおり別の問い)。

### 根拠

- **今の Kizu で受信と push を 1 つの形で両立できるのは poller loop だけ。** TaskSet
  worker の blocking read はその worker を止め、別の task が起こす primitive が
  ありません。poller loop は待つのが `wait` 1 回で、期限が来れば push の番が来ます
  (ADR-0141)。
- **`write_some` が無いと遅い reader 1 人が loop を止める。** ADR-0141 が `net` に
  `write_some` を足したときの測定は、読まない peer 1 人で loop が 1000 ms 止まった
  ものが 0 ms になりました。push は「読まない peer に書き続ける」状況そのものなので、
  `write_all` しか無い push loop は同じ穴を持ちます。`docs/std/http.md` も「大きな
  response は `write_some`」と書いています。`Exchange` にだけ無いのは、101 以前の
  HTTP には要らなかったからで、upgrade 後には要ります。
- **outbox を唯一の書き手にするのは、Go の「1 reader + 1 writer」規則を、thread の無い
  loop に移したもの。** gorilla は「one concurrent reader and one concurrent writer」
  とし、`WriteControl` だけを並行可にしています。coder は書き込みを内部で直列化
  します。どちらも制御フレームが data frame に割り込まないことを mutex で守って
  います。Kizu の loop は 1 thread なので mutex は要らず、残る危険は `write_some` の
  書きかけ frame だけで、それを outbox が frame 単位で持つことで閉じます。
- **outbox は caller の `flush` を経ないと出ない。** 「何をいつ書いたか」が source に
  残ります(原理 2)。

### 採らない案

| 案 | 理由 |
| --- | --- |
| TaskSet worker の blocking read を example の形にする | push する相手を起こす primitive が無い。今書けない形を示すことになる |
| `write_some` を足さず `write_all` だけで push する | 読まない peer 1 人で loop 全体が止まる(ADR-0141 の測定) |
| read と write を別 task に分ける(Go の 2 goroutine 形) | Kizu の TaskSet は state を move で渡すので、1 接続を 2 worker で持てない。task をまたぐ channel も無い |
| pong / close を outbox を通さず直接書く | `write_some` の書きかけ frame に割り込む(4) |

## 推奨同士の一貫性

| 組 | 一貫している点 |
| --- | --- |
| 3 と 4 | read は data を message 単位で、制御フレームは 1 つずつ返す。fragment の間に来た制御フレームは、再構成の途中でも返る(再構成の状態は Session が持ち、次の read が続きを読む) |
| 4 と 8 | read は書かず、応答は outbox に積む。だから `write_some` の書きかけ frame に割り込まない |
| 4 と 5 | 上限超過も protocol error と同じく「error を返し、close を outbox に積む」 |
| 2 と 6 | codec は sans-IO で mask key を引数に取る。乱数源は client の I/O 側だけが持つ |
| 2 と 検証 | 状態遷移は `advance` 1 つで、blocking / poller の両 read がそれを呼ぶ。Quint モデルと MBT はそこに対応する |
| 1 と 検証 | SHA-1 / base64 / UTF-8 検証は public なので、vector と差分テストを `tests/behavior` から当てられる |
| 5 と 7 | handshake は上限を取らない。上限は message を読む呼び出しにだけある |

## 残る判断

この文書で決めきれず、実装の前に要るものです。

1. **`std::crypto::sha1` を public にすること自体。** 原理 8 と検証の要求のどちらを
   取るかの判断で、推奨は「検証の要求で開く」。ユーザーの判断を仰ぐ。
2. **contract-generic な I/O 関数の書き味。** SPEC §16 は polymorphic な呼び出しに
   concrete type argument の明示を求めるので、call site は
   `websocket::read<http::Exchange>(...)` になる。この綴りが受け入れられるか、
   `Exchange` / `Connection` の両方に method を生やす形(実装は 1 つの generic を呼ぶ)に
   するか。
3. **`Received` の形。** 本文は `out` に入れ、種類と close code を struct で返す
   スケッチ。union に payload を持たせると move-only と cleanup の定型が増える
   (`docs/language-gaps.md`)ので struct を推すが、実装で確かめる。同様に、method が
   field への view を返せないので、outbox や選ばれた subprotocol は caller の
   `String` に写す形になる。
4. **`websocket::Error` の member。** 例: `MessageTooBig`、`MalformedFrame`、
   `UnmaskedFrame` / `MaskedFrame`、`InvalidUtf8`、`InvalidCloseCode`、`Closing`
   (close 後の queue)、handshake 側の `NotUpgrade`、`UnsupportedVersion`、
   `OriginRejected`、`AcceptMismatch`、`HandshakeRefused`。`Failure` は
   `Error or std::http::Failure`。名前と粒度(原理 7 でどこまで分けるか)。
5. **client の mask key の取り方。** frame ごとに `random_into` を 1 回呼ぶ(gorilla と
   同じ)か、flush 1 回でまとめて取るか。性能と「frame ごとに予測できない key」
   (RFC 6455 §5.3)の両立の確認。
6. **`Origin` の `SameHost` の比較規則。** scheme と port を含めるか(gorilla は host
   部分だけを `Host` と比べる)。
7. **`write_some` を `Connection`(client)にも足すか。** 推奨は両方(generic の
   `flush_ready` が両方に要る)。client 側の需要は Kizu 同士の push 用途だけ。
