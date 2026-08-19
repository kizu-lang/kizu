# ADR-0117: 値で受けた owner は、失敗した callee が解放する

Status: 採用

Issue: #1638

## 背景

owner を値で受ける fallible な std API が失敗すると、渡された owner が漏れる。

```kizu
try parent.append(name);   // 失敗すると name の buffer が漏れる
```

呼び出し側の IR は正しい。`name` は callee へ move 済みなので、ここで解放すれば
二重解放になる。

```text
%7: !void = call.std::array::Array.append... %6, %1
error.try %7: !void                                   ← cleanup は無くて正しい
```

漏れているのは callee 側である。runtime は確保に失敗すると element を格納も
解放もせずに捨てる。

```c
_Bool kizu_array_append(void *handle, const void *elem) {
    if (!array || !elem || !kizu_array_reserve_storage(array, array->len + 1)) {
        return 0;                       // elem をここで捨てる
    }
```

`std::mem::box` も同じで、`kizu_box_new` は確保に失敗すると `value` を書かずに
`NULL` を返す。`Array.set` は owner 要素では型 error(SPEC §9)、`Map.insert` は
value type が copy 限定なので、該当するのはこの 2 つである。

ADR-0114 は既にこの契約を前提にしていた。errdefer の退役を「move を行う
呼び出し自身が失敗する path」まで広げた理由が「その時点では callee が値を
持っているため」である。checker は callee が持つ前提で cleanup を落としており、
実装がその前提を果たしていなかった。

## 決定

**owner を値で受ける関数は、失敗 path でもその値を consume する。** 呼び出し側は
move 済みで値に触れないので、義務は callee にある。

user 関数はこれを既に守っている。ADR-0091 の「値引数の owner は全 path で
consume」を `checkOwnersConsumed` が各 error exit でも検査するためである。
守っていない関数は今も leak として拒否される。

```text
error: move error: owned value `value` would leak on this error return
```

**穴は runtime primitive にしかない。** primitive は `void *` と要素 size しか
持たず、`T` を知らないので `T` の `deinit` を呼べない。そこで **wrapper の
lowering が primitive の失敗経路に cleanup を出す。** wrapper は generic で、
monomorphize 済みの `T` がそこで束縛されている。

```text
fn std::array::Array.append.std_3a_3astring_3a_3aString(%self, %value) -> !void {
entry:
  %1: !void = array.append %self, %value, std::string::String
  error.try %1: !void, cleanup call.std::string::String.deinit %value
  %3: !void = error.ok
  return %3: !void
}
```

`error.try` + 再 wrap なので、wrapper の戻り型 `!T` は変わらず、error は
error のまま wrapper を出る。copy 要素の instance には cleanup が出ない
(`Array<u8>.append` は元のまま)。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| 呼び出し側の `error.try` に cleanup を出す | 転送で二重解放する。`fn take(value: T) -> !void { try parent.append(value); }` は内側で解放され、外側の cleanup がもう一度解放する |
| owner を返す形にする(`append(value: T) -> ?T`) | 失敗時に owner が戻るので署名が真実を言う(原理 7)。ただし全 call site が 3 行に増える(原理 10)。#1640 を閉じるまでは戻り値を捨てるだけで漏れるため、そもそも成立していなかった |
| `append` を Kizu で書き、`reserve` の失敗を観測して解放する | `E!T` を伝播せずに観測する構文が要る(言語追加)。しかも `box` には効かない。`box` は確保と初期化が 1 手で、分けると未初期化の `Box<T>` が生まれる —— ADR-0115 が `partial<T>` として却下したものである |
| 確保を先に済ませる(`reserve` してから失敗しない `append`)| 「確保済み」を compiler が証明できないので trap になる(原理 5)。`box` にも効かない |
| errdefer の退役を「呼び出しの成功時」に遅らせる | 呼び出し側に名前がある場合しか効かない。`try parent.append(try make_child(a))` は守るべき errdefer が無く、ADR-0114 が原理 1 違反として挙げた形がそのまま残る |

## 影響

- SPEC §9: 値引数の consume 義務が失敗 path も含むことを明記する
- `internal/ir`: `releaseOwnerOnFailure` が `array.append` と `box.new` の
  結果を包む。owner でない要素型では何も足さない
- `internal/ownership`: 変更なし。user 関数の義務は ADR-0091 の検査が既に持つ
- checker と実装の食い違いが消える。ADR-0114 が前提にした契約が実装される

## 再評価条件

- owner を値で受ける fallible な primitive が増えたとき。`releaseOwnerOnFailure`
  を通す先が増えるだけで済むかを確認する
- `E!T` を伝播せずに観測する構文が別の理由で入ったとき。wrapper を Kizu で
  書き直せるなら、lowering の特別扱いを畳める
