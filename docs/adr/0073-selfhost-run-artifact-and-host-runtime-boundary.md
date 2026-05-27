# ADR-0073: selfhost run artifact and host runtime boundary

Status: 採用

## 背景

Selfhost compiler の `run <file>` は、Kizu source を実際の compiler component に
通す経路でなければならない。Source literal、fixture path、静的 LLVM 文字列、
Go fallback、C runtime 内の dedicated artifact runner は、selfhost の前進ではなく
境界を曖昧にする。

一方で、現時点の Kizu は OS process、filesystem、stdio、host entry をすべて Kizu
だけで表現できない。そのため hosted 実行には小さい host ABI shim が必要である。
この shim を runtime と呼ぶ場合でも、artifact 配置、link command、run policy を
置く場所にしてはいけない。

## 決定

Selfhost hosted `run <file>` は次の pipeline にする。

```text
Kizu source
  -> selfhost frontend / checker / codegen
  -> non-static LLVM artifact in target/selfhost/cache/run/
  -> link with hosted runtime support
  -> spawn produced executable
```

`run` は LLVM IR interpreter ではない。`run` は build-like な transient execution
path であり、`.ll` artifact を生成し、それを executable にしてから実行する。

`run` が生成する artifact は cache 配下に置く。`run` 用 cache path、suffix、
linker tool、runtime input は selfhost backend の小さい artifact policy API に集約し、
CLI dispatch や C runtime に散らさない。

Generated run LLVM artifact は executable entry を持つ。`run` のためだけに
`*_hosted_run_main.c` のような C harness を生成しない。

Hosted C runtime は thin host ABI shim に限定する。

- allowed: argv/env、stdio、filesystem、process spawn、exit/trap、host init
- forbidden: source path による dispatch
- forbidden: fixture-specific branch
- forbidden: static LLVM artifact generation
- forbidden: `run_hosted_artifact` のような dedicated link-and-run helper
- forbidden: Go fallback or hidden compatibility path

Process execution primitive は artifact semantics を知らない汎用 API にする。現在の
hosted ABI では fixed-arity の `spawn_wait8(argc, args...)` でよいが、意味は
「argv を渡して child process を待つ」だけである。

## Build との違い

`kizu build` は user-visible な durable artifact を明示的に作る command であり、
target、ABI、runtime mode、linker mode、emit mode、output path を build contract と
metadata に含める。

`kizu run` は実行のための transient artifact を cache に作ってよい。ただし、
hidden fallback や static shortcut ではなく、同じ frontend/checker/backend component
を通る。Cache entry は将来、source hash、target、runtime mode、linker mode、
stdlib/runtime hash を key に含める。

## Runtime の移行方針

Zig/Rust と同様に、runtime は必ず C で大きく持つものではない。Kizu はできる部分を
Kizu source 側へ移してよい。

Kizu 側へ寄せる対象:

- path construction
- artifact naming and metadata
- CLI dispatch
- link argv construction
- std/fs/process/io abstraction

Host ABI shim に最後まで残り得る対象:

- syscall/libc boundary
- raw file descriptor and process operations
- argv/env capture
- platform startup and host init

最終形は次の境界を目指す。

```text
Kizu compiler/runtime/std code
  -> std io/fs/process capability APIs
  -> tiny hosted ABI shim
  -> libc/syscall/OS
```

## 影響

- `selfhost.hosted.c` は artifact policy の置き場ではなくなる。
- `run` parity は artifact の存在、metadata、link-and-exec behavior を確認する。
- Static route を増やす変更は regression とみなす。
- C shim を消す作業は、host boundary を Kizu std/runtime API に置き換えた場合だけ
  前進とみなす。
