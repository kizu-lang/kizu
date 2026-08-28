# チュートリアル

`docs/std/` が API 一覧、`examples/` が 1 つの機能を見せるプログラムなのに
対して、ここは **1 つのものを最初から最後まで作る**文書です。

| tutorial | 作るもの |
| --- | --- |
| [web server](web-server.md) | HTTP service 1 つ。routing、state、上限、本物の loop |

各 tutorial の sample code は同じディレクトリに Kizu package として置き、
conformance が実行します —— 末尾の case block が「実行すると何が出るか」を
宣言していて、テストがそれを確かめます。動かなくなった tutorial は嘘をつく
文書なので、信用ではなく実行で守ります。
