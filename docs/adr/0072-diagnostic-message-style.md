# ADR-0072: diagnostic message style

## 背景

Diagnostic は CLI、LSP、selfhost compiler、test oracle をまたぐ user interface で、
読むのは人間と AI の両方です。文言が場当たり的だと、同じ原因でも command ごとに違う
説明になります。strict な言語なので、失敗理由、位置、次の行動を短く出します。

## 決定

message の文言は 1 つで、形は 2 つです。

```text
<category>: <summary> at <path>:<line>:<column>     # Error()、LSP と test oracle
note: <context>
help: <action>
```

```text
error: <category>: <summary>                        # CLI(CLIError)
  --> <path>:<line>:<column>
   |
10 |     try anything();
   |         ^^^^^^^^
   = note: <context>
   = help: <action>
```

- CLI は primary span の行を source から引いて marker を付ける。column は byte で
  数え、marker の前置きは tab を残して他の文字を 1 つの空白にする。source が無ければ
  `-->` の行まで。LSP は同じ primary span を range で送る
- category は `type error` / `move error` / `unsafe error` など短く固定。parse は
  `expected ..., got ...`
- summary は 1 行で原因を直接書く。期待と実際は `expects <want>, got <got>`
- `note:` は判断の理由だけ、`help:` は次の行動が明確なときだけ
- 位置の無い front-end diagnostic は `cmd/kizu/testdata/unlocated_diagnostics.txt`
  に載っているものだけ許す(conformance test)。一覧は減るだけ

避けるもの: `mismatch` だけの message、`IDENT` のような lexer 内部名、原因と対処を
1 つの summary に詰めること、CLI と LSP で別々の message builder を持つこと。

## 却下した案

| 案 | 理由 |
| --- | --- |
| 1 行目の `at <line>:<column>` を CLI にも残す | 位置が 2 回出る。`-->` の行が path と位置を持つ |
| marker を byte 数だけ空白で揃える | 多 byte の文字の後ろでずれる。文字ごとに 1 つにする |
| 位置の無い診断を一括で直してから検査を入れる | 直している間に新しい位置無しが入る。検査と一覧を先に置く |
| `Error()` も CLI の形にする | LSP は range を別に持ち、test oracle は 1 行目で照合している |
