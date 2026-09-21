# ADR-0150: native target は OS を darwin / linux の 1 語で名乗る

## 背景

`std::target` は build family(native / wasi / browser)しか答えず、Darwin にだけ
ある framework を使う host adapter と Linux 用の adapter を 1 つの package に置けなかった。
manifest の `[native]` も OS を区別できず、Darwin の framework 探索 directory を
Linux の build にも渡していた。

## 決定

native target は OS を持つ。`std::target::is_darwin()` / `is_linux()` を `is_native()`
と同じ comptime-if-only 述語として足し、manifest は `[native.darwin]` / `[native.linux]`
で OS 別の探索 directory を `[native]` に加える。述語と section は同じ OS 名を使い、
その名前は compiler が 1 か所(`stdtarget`)で持つ。OS は `--triple` か host から
1 回決め、comptime 選択・manifest・link がその 1 つの答えを読む。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| `std::target::os()` が文字列を返し `== "darwin"` で比べる | comptime 文字列比較の経路を増やし、typo が false になるだけで診断されない |
| `is_macos()` | triple・Go・uname はどれも darwin と言う。macOS 以外の Darwin を別 OS にする理由も無い |
| `[target.'cfg(...)']` のような条件式 section(Cargo) | manifest に式評価器が要る。OS 名 1 語で足りる |
| triple を section 名にする | 同じ OS の triple が複数あり、host build は triple を持たない |
| 未対応 OS を linux 扱いにする | hidden fallback。runtime を build できない OS は error で閉じる |
