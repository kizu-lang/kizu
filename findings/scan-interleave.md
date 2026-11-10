# ベクトル化ループの interleave 差の切り分け(2026-11-10)

配列を一方向に舐める `sum_max` の scan(`[]i64` の sum と max を 1 pass)が
Rust の 1.5〜1.9 倍だった件。両方とも NEON にベクトル化されるが、Rust は
1 反復 8 要素(VF 2 × interleave 4)、Kizu は 4 要素(VF 2 × interleave 2)。

## 結論

LLVM の版でも、Kizu が IR に付ける属性でもない。`--opt` の arm64 で clang に渡して
いる `-Xclang -target-feature -Xclang -fuse-literals` が原因。

LLVM の subtarget feature は、ある feature を外すとそれを含意する feature も外す。
`fuse-literals` は Apple の tuning 一式(`apple-m1` 以降の CPU が持つ
`apple-a14` 等)に含意されているので、外すと tuning 一式ごと落ち、CPU が
`apple-m1` のままでも loop vectorizer の上限 interleave が Apple の 4 から
既定の 2 に下がる。

## 切り分け

同じ Kizu の `.ll`(`kizu build --emit-llvm --opt`)を使った。数字は vectorizer の
remark(`interleaved count`)か、asm の loop 内の `cmgt.2d` の本数。

| 経路 | interleave |
| --- | ---: |
| Apple clang 17 `-O3` | 4 |
| clang 21.1.8 `-O3` | 4 |
| どちらかの clang + Kizu の `--opt` flag 一式 | 2 |
| `-fuse-literals` だけ外す | 4 |
| `-fuse-literals` だけ足す | 2 |
| opt 21 `-mcpu=apple-m1` | 4 |
| opt 21 `-mcpu=apple-m1 -mattr=-fuse-literals` | 2 |
| opt 21 `-mcpu=generic`(または `-mcpu` 無し) | 2 |
| `"target-cpu"="apple-m1"` を関数属性に足す(flag 一式のまま) | 2 |

clang は IR 入力にも `-target-cpu apple-m1` を渡しているので、target-cpu 属性を
足しても変わらない。alignment / noalias は版と flag で差が出入りした時点で
原因から外れる。

## 直し方の比較

`fuse-literals` は、f64 定数を mov + movk ×3 で作らず literal pool から読ませる
ためのもの(定数の多い kernel を呼ぶ loop で効く)。外すとそれを失う。

| 案 | sum_max scan / Rust | 定数 12 個の再帰 kernel |
| --- | ---: | ---: |
| 現状 | 1.92 | 41 ms |
| `-fuse-literals` を外す | 1.08 | 48 ms(+17%) |
| 残したまま `-force-target-max-vector-interleave=4` を足す | 1.08 | 41 ms |

3 案目を採る。落ちた tuning のうち、この差を生んでいた上限 interleave だけを
Apple の値に戻す。
