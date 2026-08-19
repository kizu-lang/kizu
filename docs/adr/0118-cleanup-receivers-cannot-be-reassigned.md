# ADR-0118: cleanup receiver に再代入できない(ADR-0114 決定 4 の置換)

Status: 採用

Issue: #1642

## 背景

ADR-0114 決定 4 は「退役した receiver への再代入で errdefer は復活しない。
新しい owner は新しい義務を持つので、必要なら errdefer を書き直す」と決めた。

実装されていなかった。再代入すると cleanup が復活し、しかも**再代入前の値**を
指したままだった。

```kizu
var name = string::new(allocator);
errdefer name.deinit();
try parent.append(name);        // 退役
name = string::new(allocator);  // 再代入
try might_fail(false);          // error path
```

```text
== 別 binding で書いた正しい形 ==   runtime error: E::Boom     exit 1
== 上の再代入 ==                    exit 134                   SIGABRT
```

`check: ok` で、診断は何も出なかった。IR に原因が出ている。

```text
%7  = call.Array.append %1, %2
error.try %7, cleanup call.Array.deinit_all %1              ← 退役している
%9  = call.std::string::new %allocator                      ← 新しい String
error.try %12, cleanup call.String.deinit %2; ...           ← 復活。%2 を指す
```

`%2` は `%1` の所有なので、同じ cleanup list の `deinit_all %1` と合わせて
同じ buffer を 2 回解放する。新しい `%9` はどの exit でも解放されない。

同じ穴が `defer` にもあった。`defer` receiver への再代入も `check: ok` で、
cleanup は古い SSA 値を指したままだった。

## 決定

**`defer` / `errdefer` の receiver に別の値を代入することを compile error に
する。** ADR-0114 決定 4 を置き換える。復活しない、ではなく、代入させない。

cleanup は登録時に live だった値を解放する。名前が別の値を指すようになると、
cleanup は名前が意味しなくなった値を持つ。1 つの名前に 1 つの値、1 つの
cleanup にすれば、この乖離が生まれる場所がなくなる(原理 7)。

## 他言語

| | cleanup が指すもの | 再代入したら | 古い値 |
| --- | --- | --- | --- |
| Rust / C++ / Swift | 値(自動) | 代入の地点で古い値を drop | 安全。ただし解放が source に現れない |
| Zig / Go / Odin | 名前(exit 時に読み直す) | 新しい値を解放 | 黙って漏れる |
| Kizu | 値(手動) | **compile error** | 起きない |

Zig 0.16 で実測した。`errdefer` は名前を追い、再代入後は新しい値を解放して、
古い値は漏れる(`DebugAllocator` が leak を報告する)。手動 cleanup は普通
名前に付ける —— 値に付けるには「いつ値が名前から離れたか」を追う move 追跡が
要るからで、Zig と Go にはそれがない。

Kizu は move を持つので値に付けられる。それが Zig の leak と Rust の隠れた
解放の両方を避けている理由であり、同時に「綴りは名前、意味は値」という乖離を
生む理由でもある。禁止はその乖離が出る唯一の場所を閉じる。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| ADR-0114 決定 4 のとおり実装する(復活させない) | 動くが、同じ 1 行 `errdefer name.deinit();` が 2 か所で違う値を指す形を許す。実装も checker の retired flag と lowering の cleanup 除去の 2 本が要る。実際、退役を名前で IR へ伝える設計だったため、書き直した errdefer まで巻き添えで落ちる二次バグが出た |
| Rust のように代入の地点で古い値を解放する | 解放が source に現れない(原理 2)。Kizu が明示 cleanup を選んだ前提と矛盾する |
| Zig のように cleanup を名前に付け、exit 時に読み直す | move 済みの値しか読めない。Kizu の move 追跡と両立しない |
| `defer` は許し `errdefer` だけ禁止する | 同じ穴が `defer` にもあった。理由が同じものを別扱いにする(原理 9) |

## 影響

- SPEC §6.3.1: 「復活しません」を「compile error」に置き換える
- `internal/ownership`: `checkCleanupReceiverOverwrite` が代入時に拒否する
- `internal/ir`: 変更なし。拒否される形は lowering に届かない
- ADR-0114 決定 4 を置換する。決定 1・2 と ADR-0116 はそのまま

既存コードへの影響はない。禁止を先に実験で入れて全 test を回したところ、
落ちたのはこの穴のために書いた example と test だけだった。

## 再評価条件

- 同名再利用が実需になったとき。loop 本体の `var` は毎回新しい binding なので
  再代入ではなく、今のところ需要が出ていない
- 自動 drop を入れる判断をしたとき。そのときは禁止ごと不要になる
