# std::compress

deflate(RFC 1951)と、それを運ぶ 2 つの container —— zlib(RFC 1950)と
gzip(RFC 1952)—— を読み書きします。HTTP の `Content-Encoding: gzip` と
`deflate` がこの 2 つです。

```text
std::compress::inflate(allocator: Allocator, bytes: []u8, limit: std::mem::Limit) -> Failure!String
std::compress::gunzip(allocator: Allocator, bytes: []u8, limit: std::mem::Limit) -> Failure!String
std::compress::unzlib(allocator: Allocator, bytes: []u8, limit: std::mem::Limit) -> Failure!String
std::compress::inflate_into(allocator: Allocator, bytes: []u8, out: &var String, limit: std::mem::Limit) -> Failure!void
std::compress::gunzip_into(allocator: Allocator, bytes: []u8, out: &var String, limit: std::mem::Limit) -> Failure!void
std::compress::unzlib_into(allocator: Allocator, bytes: []u8, out: &var String, limit: std::mem::Limit) -> Failure!void

std::compress::deflate(allocator: Allocator, bytes: []u8) -> std::mem::Error!String
std::compress::gzip(allocator: Allocator, bytes: []u8) -> std::mem::Error!String
std::compress::zlib(allocator: Allocator, bytes: []u8) -> std::mem::Error!String
std::compress::deflate_into(allocator: Allocator, bytes: []u8, out: &var String) -> std::mem::Error!void
std::compress::gzip_into(allocator: Allocator, bytes: []u8, out: &var String) -> std::mem::Error!void
std::compress::zlib_into(allocator: Allocator, bytes: []u8, out: &var String) -> std::mem::Error!void

std::compress::Error    Malformed | Truncated | ChecksumMismatch | LimitExceeded
std::compress::Failure  Error or std::mem::Error
```

```kizu
let text = try compress::gunzip(allocator, body, mem::Limit::Bytes(1 << 20));
defer text.deinit(allocator);

let plain = text.as_bytes();
let member = try compress::gzip(allocator, plain);
defer member.deinit(allocator);
```

`inflate` は container の無い生の deflate stream、`gunzip` は gzip、`unzlib` は
zlib を読みます。`deflate` / `gzip` / `zlib` はそれぞれの対になる書き手です。
どれも入力全体を bytes で受け取り、結果の bytes を String に append します。
`_into` は呼び手の String に、それ以外は新しい String に append して owner
として返します。

## 上限は呼び手が決める

`limit` は省けません。deflate は 2 bit で 258 byte を名指しできるので、小さな
stream が巨大な出力を要求できます。どこまでを受けるかは、その bytes が何で
どこから来たかを知っている呼び手の判断で、library には決められません。上限は
**この呼び出しが append する量**に掛かります。`_into` の String に元からあった
bytes は数えません。超えると `LimitExceeded` で、出力はそこで止まります。

`std::mem::Limit::Unlimited` も選択のひとつで、そう書いたことが source に残ります。

## 失敗したとき

| 状況 | error |
| --- | --- |
| 定義されていない block type、prefix code に載らない code length の組、出力の先頭より前への参照、補数と合わない stored block の長さ、container の magic や予約 flag の不一致、stream の終わりの後に続く bytes | `Malformed` |
| stream の途中で入力が終わった | `Truncated` |
| gzip の CRC-32 か長さ、zlib の Adler-32、header CRC を持つ gzip のその値が、展開した bytes と合わない | `ChecksumMismatch` |
| 出力が `limit` を超える | `LimitExceeded` |
| 出力の確保に失敗 | `std::mem::Error::OutOfMemory` |

`Truncated` を `Malformed` と分けているのは、stream の一部しか手元に無い呼び手が
「まだ足りない」と「壊れている」を区別できるようにです。`ChecksumMismatch` は
stream 自体は最後まで読めた、という意味です。bytes は送り手が check した
ものと違っています。

失敗したとき、`_into` の String には失敗の手前まで append した bytes が
残ります。owner を返す形は String を解放し、error だけを返します。

zlib の preset dictionary(FDICT)は `Malformed` です。辞書を渡す口が無いので、
この decoder が読める stream ではありません。

書く側が失敗するのは出力の確保だけ(`std::mem::Error`)で、上限も取りません。
stream が入力より大きくなるのは高々 block ごとの数 byte で、入力を手に持って
いる呼び手にとって新しい量ではないからです。

## 書く側に level は無い

`deflate` は入力の中に前に出た bytes の繰り返し(back-reference)を探し、
block ごとに 3 つの符号化 —— block 自身が記述する Huffman code、固定 code、
そのまま —— の中で bit 数が最も少ないものを選びます。探索は window 32 KiB、
hash chain を 64 step までです。

zlib や Go の `compress/flate` が持つ level は、この探索をどこまで粘るかの
つまみです。最速と最小の差は出力の数 % で、代償は数倍の時間です。Kizu では
1 つの形だけを持ち、それが reference encoder の default level と同じ程度の
大きさを書くことを `tests/behavior/src/compress/` で見ています。それより
小さくしたい呼び手に必要なのは、つまみではなく新しい format です。

書き出した stream は自分の reader が読み戻すだけでなく、外の reader にも
読ませます。`examples/compress_gzip.kizu` が印字する member を、Go の
`compress/gzip` が開くことを `cmd/kizu` の test が見ています。

| 入力 | stream |
| --- | --- |
| 空 | 2 byte(最終 block が 1 つ、すぐ終わる) |
| 乱数のような bytes | 入力 + block(16384 token)ごとに 5 byte |
| gzip | deflate + header 10 byte + trailer 8 byte |
| zlib | deflate + header 2 byte + trailer 4 byte |

gzip の header は format 以外を何も言いません。名前も時刻も無く、OS は
「不明」(255)です。何を言うかは呼び手が知っていることで、library が
勝手に書くものではないからです。

## gzip の member

gzip file は member の連結でよく(`gzip -c a b`)、`gunzip` は続く member を
全部読みます。header の extra field、名前、comment は読み飛ばし、header CRC
があれば検査します。member の後の CRC-32 と長さ(mod 2^32)は、その member が
展開した bytes と比べます。

## 入力を信用しない

decoder は「どの入力にも `Error` か bytes を返す」ように書かれています。
すべての loop は入力 bit を消費するか出力 byte を出すかのどちらかで、bit を
読み切れば `Truncated`、出力は `limit` で止まります。table の添字は構築時に
範囲を確かめ、出力への参照は「この stream が書いた範囲」に限ります。
`tests/behavior/src/compress/` は reference encoder の stream と、手で組んだ
壊れた stream、乱数で 1 byte 変えた stream を流し、どれも trap せず error か
bytes で返ることを見ています。

## 今は話さないこと

- **streaming**: bytes を少しずつ受け取りながら展開・圧縮すること。今は入力
  全体を手に持ってから始めます。
- **preset dictionary**。
- **gzip header の name / time / comment** を書くこと。
