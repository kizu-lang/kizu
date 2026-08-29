# ADR-0137: deadline は時点で、socket の owner が持つ

## Status

Accepted.

## Context

`std::net` と `std::http` を入れた直後の実装には timeout がどこにもありませんでした。
接続して黙るだけで service が止まります。

```console
$ printf 'GET / HTTP/1.1\r\n' | nc 127.0.0.1 8080 &
$ curl -m 5 http://127.0.0.1:8080/
$ echo $?
124
```

並行 API が無い(SPEC §15)ので 1 接続ずつしか捌けず、その 1 本が返らない限り
2 人目は listen backlog で待ち続けます。並行性を待たずに閉じられる穴です。

決めることが 3 つありました。**何で測るか**、**どこに置くか**、**何と名乗るか**。

## Decision

### 何で測るか: poll と絶対時点、そして non-blocking な descriptor

deadline は**時点**です。1 回の呼び出しに配り直される budget ではありません。

Kizu 側が絶対 monotonic ms を持ち、呼び出しごとに残りを計算して primitive に
渡します。primitive は `poll` してから `recv` / `send` / `accept` し、残りが
尽きていれば `Error::TimedOut` を返します。deadline が無ければ `poll` が無期限に
待つので、呼ぶ側から見た挙動は変わりません。

**descriptor は non-blocking です。** これが無いと deadline は効きません ——
blocking な `send` は部分書き込みで返らず、渡された分が全部入るまで kernel に
留まるので、読まない peer への 4 MB の write は poll も deadline も一度も読まれ
ないまま止まります(実測: 期限切れの deadline でも返らず)。`MSG_DONTWAIT` は
Darwin の stream socket では無視されるので、答えは `O_NONBLOCK` でした。

待つのを syscall から `poll` に移しただけなので、`WouldBlock` は Kizu に出ません。
`write_all` は今も「全部書くか失敗するか」です。

### どこに置くか: owner の field

`TcpStream` / `TcpListener` の field です。kernel でも、C 側の fd を key にした
表でもありません。

### 何と名乗るか: `set_read_deadline(net::deadline_in_millis(5000))`

setter は Go の名前をそのまま取り、絶対時点を受け取ります。相対から時点への
変換は `deadline_in_millis` という別の関数が持ちます。

## Consequences

phase ごとに押し直す責任が呼ぶ側に来ます。`std::http` はそれを `Limits` の
duration から各 phase の開始時に作ります —— policy(`Limits`)と、その policy の
この 1 回分(deadline)は別のものです。

keep-alive を入れるときは、message と message の間で押し直す必要があります。
押し直しを忘れた実装は 2 本目が期限切れで始まり、それは deadline という語が
警告している通りの失敗です。

`connect` にはまだ deadline がありません。non-blocking connect と `POLLOUT` と
`getsockopt(SO_ERROR)` が要り、上の 3 つとは別の仕組みだからです。黒穴宛ての
`tcp_connect` は今も host の既定(~75s)まで固まります。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| descriptor を blocking のままにする | deadline が効かない。blocking な `send` は渡された分が全部入るまで返らず、その間 poll も deadline も読まれない。実測で 4 MB の write が期限切れの deadline を無視して止まった |
| `MSG_DONTWAIT` を send に付けて blocking のままにする | Darwin が stream socket で無視する。probe を仕込んで確認済み |
| non-blocking を Kizu に見せる(`WouldBlock` error、`write_some`) | 待つのを runtime の `poll` に置けば呼ぶ側は何も変わらない。API を増やすのは「戻って他の仕事をする」が要るときで、それは poller と組み合わせて初めて意味がある |
| `setsockopt(SO_RCVTIMEO / SO_SNDTIMEO)` | kernel の timeout は **1 syscall ごと**。5s に設定しても 4s ごとに 1 byte 送る相手は永久に引っかからず、防ぎたかった相手そのものが通る。Rust std / Python / Java がこの形で、同じ穴を持つ |
| 相対 duration を stream の field に持つ | 保存の形が相対だと、read のたびに「今から N ms」が復活する。上と同じ穴を Kizu 側に作り直すだけ |
| deadline を各呼び出しの引数にする | 隠れた状態は無くなるが、deadline は socket にしかなく `read` は全部にある概念。SPEC §16 の `contract` を作ったとき、memory buffer 上の reader まで deadline 引数を要求されることになる。4 言語が揃って setter なのはこの理由で、Kizu も同じ制約を受ける |
| C 側に fd → deadline の表を持つ | 状態が Kizu の型から消え、fd 再利用で他人の deadline を拾う。field に持てば型が「この呼び出しが変えた」と言う |
| setter を `set_read_timeout(ms)` にする | `timeout` は「1 回 set して忘れる」ものに聞こえ、keep-alive で 2 本目が期限切れの状態で始まる書き方を誘う。`deadline` は時点なので「押し直さないと切れる」が語に含まれる |
| setter を `set_read_deadline_in(ms)` にする | 相対と絶対の区別は付くが、`setTimeout` の影で「N ms 後に set する」と読める。値を返す `deadline_in_millis` に `_in` を移せば、返り値は何も schedule しないので誤読が消える |
| `Limits` を持たず、時間も deadline として受け取る | policy(server が一生許す長さ)と、この 1 回の deadline(いつまでか)は別物(原理 7)。`Limits` を毎回作り直させることになる |
| timeout の既定を 0(無期限)にする | Go の既定がこれで、忘れた server は 1 接続で止まる。既定は数値にし、0 は「置かない」と明示的に書いた caller にだけ渡す(原理 3) |
| head と body で deadline を共有する | 大きな body が「head の余り」しか貰えない。phase ごとに始まりで作る |
