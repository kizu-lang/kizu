# ADR-0124: 複製は常に明示。owner の複製は per-type の copy 関数で書く

Status: 採用

## 背景

owner 型(`Array<T>`、`Map<K, V>`、`String`、`Box<T>`)は copy できず move
される(SPEC §8)。複製が要る場面でどう書くかを決める。

複製を汎用化する機構は無い。`Clone` trait と trait bound は持たない。closure は
local を capture できず runtime data に保持できないので、`clone_with(複製関数)`
のような高階 API も書けない。copy 型はビット複製で、hook は無い。

## 決定

**複製は常に明示**とし、所有性で 2 層に分ける。

1. **暗黙 clone、`Clone` trait、owner 要素の汎用 deep clone は提供しない。**
   owner 要素の複製は要素ごとの複製ロジックを要するが、それを渡す綴りが言語に
   無い。無理に builtin にしない。

2. **owner を持つ container** は per-type の copy 関数で複製する。各要素を要素
   自身の複製関数で deep-copy し、新しい container へ組み直す。

   ```kizu
   pub fn copy_names(src: &array::Array<string::String>)
       -> !array::Array<string::String> {
       var out = array::new<string::String>(allocator);
       errdefer out.deinit();
       var index = 0;
       while src.at(index) |name| {
           try out.append(try copy_name(name));
           index = index + 1;
       }
       return out;
   }
   ```

3. **copy 要素 / copy value の container** は、必要になったら `clone()` を
   copy 限定の builtin として足してよい。buffer のビット複製で安全かつ安価で
   ある。checker は `Array.get` / `Map.get` と同じ copy 判定で縛る。任意の
   利便機能で、明示的な組み直しでも代替できる。

## 意味

`[]u8` 要素の clone は view の複製である。`[]u8` は所有しない copy view
(ptr + len)なので、背後の bytes は複製されず両方が同じ backing を指す。所有者
ではないので double-free にはならない。背後まで複製したいなら明示的に新しい
buffer へ copy する。

copy 要素では shallow と deep が一致する。それ以上の所有を持たないためである。
入れ子の owner(`Array<Array<T>>`)は各層の copy 関数を組み合わせて書く。derive
は行わない。

## 影響

ADR-0123 と一対である。`Map` は owner value を持てるが、その複製は per-type の
明示に留まる。持てることと、勝手に複製されることは別である。
