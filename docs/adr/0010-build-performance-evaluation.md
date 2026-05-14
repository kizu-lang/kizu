# ADR-0010: ビルド時間とキャッシュサイズの評価方法を早期に確立する

Status: 採用

## 背景

Kizu は CI とビルドキャッシュが重くなりにくい言語を目指す。
Rust のようにビルドキャッシュが数十 GB まで膨らむ体験は避けたい。

この性質は、後から最適化するだけでは保証しにくい。
処理系の初期段階から、コンパイル時間、実行時間、キャッシュサイズ、再ビルド理由を測れる状態にする必要がある。

## 決定

ビルド時間とキャッシュサイズの評価方法を、compiler backend より前に確立する。

評価対象は次に分ける。

- cold run: キャッシュなし
- warm run: キャッシュあり
- no-op rebuild: 入力変更なし
- single-file edit rebuild: 1 ファイルだけ変更
- dependency edit rebuild: 依存側を変更
- cache size: キャッシュ総量
- cache entries: キャッシュ項目数
- why-rebuild: 再ビルド理由

将来の CLI は次を持つ。

```sh
kizu cache status
kizu cache prune
kizu why-rebuild <file>
```

## 方針

- キャッシュにはデフォルト上限を持たせる
- キャッシュ測定を CI で実行できる形にする
- benchmark は wall clock だけでなく、入力サイズと cache size も記録する
- debug artifact と optimized artifact は cache key を分ける
- build script と proc macro は採用しない
- 巨大な中間生成物は opt-in で保存する
- performance regression は GitHub Issues の受け入れ条件で扱えるようにする

## 初期の測定対象

処理系が interpreter 段階の間は、次を測る。

- `go test ./...`
- `go run ./cmd/kizu parse examples/hello.kizu`
- `go run ./cmd/kizu run examples/hello.kizu`
- `pre-commit run --all-files`

compiler backend が入った後は、次を追加する。

- `kizu build`
- `kizu ir`
- LLVM IR generation
- WASM / WASI generation
- cache hit / miss

## 影響

- `docs/perf.md` を性能評価の入口にする
- performance 関連 TODO は GitHub Issues で管理する
- backend や cache 関連 issue は、受け入れ条件に性能測定または cache size 確認を含める
- 最適化を入れる前に、測定可能な状態を作る
