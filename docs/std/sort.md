# std::sort

owner を含む配列を、明示的な mutable borrow 越しに並べ替える module です。

```text
std::sort::strings(values: &var Array<String>) -> std::array::Error!void
```

`strings` は `String.as_bytes()` の byte lexical order で昇順に並べます。空文字、
prefix、重複を含められ、同値要素間の順序は保証しません。処理は in-place の
heapsort で、追加 allocation を行わず、worst-case `O(n log n)` です。

要素の移動には owner-safe な `Array.swap` を使うため、要素を copy・replace・
cleanup しません。`std::array::Error!void` は checked swap の error を明示する
ための形ですが、
algorithm が作る index は常に範囲内です。内部の heap index invariant が崩れた
場合は trap し、通常の入力内容は error にしません。

汎用 comparator API はまだ公開しません。callable / comparator の契約を言語側で
決めずに `sort_by` だけを先行させないためです。
