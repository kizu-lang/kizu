# 種(seed)からの build

`compiler/` は、前のリリースのバイナリ(種)で build できる範囲に保ちます。
compiler を build する経路はこれだけなので、CI で毎回確かめます。

## 種の pin

種は `scripts/seed.txt` が 1 か所で固定します。リリースのバージョンと、
プラットフォームごとのアーカイブの sha256 です。

`scripts/fetch-seed.sh` がそのアーカイブを
`https://github.com/kizu-lang/kizu/releases` から取り、sha256 が pin と合わなければ
展開せずに失敗します。展開先は `target/seed/` です。

## 流れ

`just selfhost`(= `scripts/bootstrap.sh`)が回すもの:

1. 種で `kizu check compiler` と `kizu test compiler`。種は自分のアーカイブに
   入っている std(`lib/kizu`)を読む。`KIZU_LIB_DIR` は空にして渡す
2. 種で現行の compiler を build して stage1 を得る
3. stage1 で `kizu check compiler` と `kizu test compiler`、
   `scripts/check-sources.sh`(`kizu fmt` との一致と、std::tls から生成する
   spec の表の鮮度)。stage1 は現行の `lib/kizu` を読む
4. stage1 で test harness(`compiler/tests/harness`)の test と behavior suite
5. stage1 で stage2 を build し、stage2 が自分自身を byte 単位で再現すること

CI の必須 job `test (ubuntu-latest)` と `test (macos-15)` は、どちらも同じ
スクリプトを全部回します。native の link は host の clang を通るので、両方の
platform で build と test をします。

`--check-only` は種と stage1 の check だけで止まり、commit hook が使います
(手元では `just selfhost-check`)。`--stage1-only` は stage1 を build するだけで、
`just stage1` と、`just run` など stage1 を使う recipe、release の build が使います。
stage1 は `target/seed/stage1/bin/kizu` に置かれます。

## 規律: 破壊的変更は 2 リリースがかり

`compiler/` が新しい言語機能や新しい std API を使うと、種がそれを知らないので
build できなくなります。その変更は 2 回に分けます。

1. compiler と std にその機能を入れる。`compiler/` 自身はまだ使わない。リリースする
2. pin をそのリリースに上げる。それから `compiler/` で使う

std の API を消す・変えるときも同じです。種が build に使うのは種自身の std なので、
`compiler/` が使っている API はまず `compiler/` 側を移してから消します。

## pin の上げ方

新しいリリースが出たら:

1. リリースの全アセットを落とし、`shasum -a 256`(Linux は `sha256sum`)で
   自分で計算する。GitHub の release API が返す `digest` とも突き合わせる
2. `scripts/seed.txt` の `version` と各 `sha256` を書き換える
3. `just selfhost` を通す
4. pin の変更だけの commit にする

pin は直前のリリースでなくてもかまいません。上げるのは、`compiler/` が新しい
機能を使いたくなったときで十分です。
