# std::compress

deflate(RFC 1951)と、それを運ぶ 2 つの container —— zlib(RFC 1950)と
gzip(RFC 1952)—— を読みます。HTTP の `Content-Encoding: gzip` と `deflate`
がこの 2 つです。書く側(圧縮)はまだありません。

```text
std::compress::inflate(allocator: Allocator, bytes: []u8, limit: std::mem::Limit) -> Failure!String
std::compress::gunzip(allocator: Allocator, bytes: []u8, limit: std::mem::Limit) -> Failure!String
std::compress::unzlib(allocator: Allocator, bytes: []u8, limit: std::mem::Limit) -> Failure!String
std::compress::inflate_into(allocator: Allocator, bytes: []u8, out: &var String, limit: std::mem::Limit) -> Failure!void
std::compress::gunzip_into(allocator: Allocator, bytes: []u8, out: &var String, limit: std::mem::Limit) -> Failure!void
std::compress::unzlib_into(allocator: Allocator, bytes: []u8, out: &var String, limit: std::mem::Limit) -> Failure!void

std::compress::Error    Malformed | Truncated | ChecksumMismatch | LimitExceeded
std::compress::Failure  Error or std::mem::Error
```

```kizu
let text = try compress::gunzip(allocator, body, mem::Limit::Bytes(1 << 20));
defer text.deinit(allocator);
```

`inflate` は container の無い生の deflate stream、`gunzip` は gzip、`unzlib` は
zlib を読みます。どれも stream 全体を bytes で受け取り、展開した bytes を
String に append します。`_into` は呼び手の String に、それ以外は新しい String
に append して owner として返します。

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

- **圧縮**(deflate / gzip / zlib を書く側)。
- **streaming**: bytes を少しずつ受け取りながら展開すること。今は stream 全体を
  手に持ってから読みます。
- **preset dictionary**。
