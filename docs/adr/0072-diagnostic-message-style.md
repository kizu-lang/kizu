# ADR-0072: diagnostic message style

Status: 採用

## 背景

Kizu の diagnostics は CLI、LSP、selfhost compiler、test oracle をまたぐ user
interface です。文言が場当たり的だと、同じ原因でも command ごとに違う説明になり、
LSP と CLI の修正もずれます。

Kizu は strict な language なので、diagnostic は長い説明で隠すのではなく、
失敗理由、位置、次の行動を短く出す必要があります。

## 決定

User-facing diagnostic は次の形を標準にします。

```text
<category>: <summary> at <line>:<column>
note: <context>
help: <action>
```

`at <line>:<column>` は primary span を持つ diagnostic に付けます。File path、
caret、色、related span の表示は renderer の責務です。LSP は同じ primary span を
range として送ります。

Category は短く固定します。

- `type error:`: type checker の失敗
- `move error:`: ownership / move checker の失敗
- `unsafe error:`: unsafe capability boundary の失敗
- parse diagnostics: `expected ..., got ...` の summary を使い、CLI renderer が
  `error:` severity を付けます

Summary は 1 行で、原因を直接書きます。期待値と実値がある場合は
`expects <want>, got <got>` または `expected <want>, got <got>` を使います。
Binary operator の型不一致は可能なら operand ごとに `note:` を出します。

`note:` は「なぜそう判断されたか」の補足にだけ使います。

```text
note: left operand has type Color
note: right operand has type Animal
```

`help:` は「次に何をすればよいか」が明確な場合だけ使います。

```text
help: `@unsafe(ptr_read)` permits raw pointer reads with `ptr_read(p)`.
```

`warning:` は compile / run を止めないが、将来壊れる可能性が高い、危険、
または未使用であるものに限定します。v0 では warning infrastructure を広げず、
warning を追加する場合はこの ADR の形に揃えます。

## 避ける表現

- `mismatch` だけで終わる message
- `IDENT` / `RBRACE` のような lexer 内部名を user-facing message に出すこと
- 原因と対処を同じ長い summary に詰め込むこと
- CLI と LSP で別々の message builder を持つこと

## 影響

- Go compiler と LSP は同じ diagnostic text / primary span を共有します。
- Selfhost 側も新しい diagnostic を追加するときはこの style に合わせます。
- Existing selfhost golden は一括 rewrite せず、触る diagnostic slice ごとに
  oracle と一緒に移行します。
