# ADR-0088: map は挿入順で反復し、lookup は O(1)

Status: 採用

## 背景

`std::map` の runtime は entries の線形走査だった。

```c
static int64_t kizu_map_find(KizuMap *map, const unsigned char *key, int64_t key_len) {
    for (int64_t i = 0; i < map->len; i += 1) {
        if (map->entries[i].key_len == key_len && memcmp(...) == 0) return i;
    }
    return -1;
}
```

`map` という名前で O(n) を出していた。利用者が書いた O(n) のループは黙って
O(n²) になる。実測でも二次だった。

```
n=10,000  0.45s   n=20,000  0.79s   n=40,000  2.22s   n=80,000  7.98s
```

同時に、反復 API をまだ持っていない。つまり反復順を何も約束していない。
決めるなら、約束が生まれる前の今しかない。

## 決定

### 1. 反復順は挿入順である

未定義にはしない。選択肢は 3 つあり、真ん中が最も悪い。

| | |
|---|---|
| 実行ごとに乱択する | 依存は防げるが、実行ごとに挙動が変わる。reproducible を保証する言語には合わない |
| 未定義だが実際は安定 | 潜伏する。変数を rename しただけで出力が変わり、原因が hash 実装だと気づけない |
| 定義する | 説明が 1 行で済み、利用者が予測できる |

Go が実行ごとの乱択という高い代償を払っているのは、未定義のまま出した後で
互換性に縛られたからである。Kizu は最初から定義すれば、その代償を払わずに
同じ保証を得る。

同じ判断を LLVM は `MapVector`、rustc は `FxIndexMap`、Zig は `ArrayHashMap`、
Python は dict 3.7 で下している。

### 2. 実装は entries 配列 + index table である

```c
KizuMapEntry *entries;  /* 挿入順。反復と順序はここから来る */
int64_t *index;         /* 開番地法。hash から entries の添字へ */
```

順序のために何かを足すのではない。O(1) 化の自然な形が順序を保つので、
**挿入順は副産物として無料で手に入る**。順序と O(1) を別々に決めると
map を 2 回書くことになるため、1 つの決定として扱う。

entry は自分の hash を持つ。index を広げるとき key bytes を読み直さない。

### 3. 削除は名前を 2 つに分ける

削除 API はまだ無い。足すときは最初から分ける。

- `remove` — 順序を保つ。O(n)
- `swap_remove` — O(1)。順序を壊すと名前で言っている

Zig の [#7696](https://github.com/ziglang/zig/issues/7696)
"ArrayHashMap loses insertion order upon delete" は、この区別を後から入れた
結果である。先に決めておけば踏まない。

## 影響

- `internal/native/build.go` の C runtime のみ。`internal/llvm/map.go` は同じ
  `@kizu_map_*` を呼ぶだけで変更なし
- `Hash` contract は要らない。key はどれも占めている byte 列として hash
  されるので、利用者の型を hash する規則を今決める必要がない
- tombstone は要らない。削除 API がまだ無い
- SPEC.md に「map は挿入順で反復する」「insert / get / contains は amortized O(1)」

## 一般化

言語として持つ規律は 1 つである。**定義していない順序を露出しない。**
map だけの話ではなく、struct field や error set member を将来反復させる
API を足すときも同じ規則が効く。
