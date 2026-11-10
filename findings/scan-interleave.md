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

## 計測(`bench/rust/run.sh 11`、変更前後を交互に 2 セットずつ)

Apple M4 Max、Apple clang 17.0.0、rustc 1.91.1。compiler は main `7a06a6c` と
この変更をそれぞれ `seed build --target native --opt` で build した self-host。
チェックサムは全ベンチ・全セットで両言語一致。生成 binary が変わったのは
sum_max と matmul だけで、他の 4 つは byte 単位で同一。

| フェーズ | 変更前 Kizu ms(比) | 変更後 Kizu ms(比) |
| --- | --- | --- |
| sum_max scan | 36 (1.03) / 41 (2.56) | 21 (1.05) / 19 (1.06) |
| matmul × 10 | 54 (1.00) / 55 (1.04) | 56 (0.93) / 55 (1.02) |
| sum_max fill | 169 (0.99) / 184 (1.10) | 168 (1.00) / 168 (1.01) |

scan はメモリ律速で両言語ともぶれが大きい(変更前 1 セット目は Rust 側が
23〜46 ms と荒れて比が 1.03 になった)。同じ時間帯に 3 案を並べた上の表では
1.92 → 1.08。
