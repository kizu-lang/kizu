# ADR-0154: Windows target は GNU 環境で、llvm-mingw が link する

## 背景

Windows の C 環境は 2 つある。MSVC(`x86_64-pc-windows-msvc`)と GNU
(`x86_64-w64-windows-gnu`)で、clang はどちらも作れるが、link に要る C runtime と
import library の出どころが違う。

## 決定

* Windows target は GNU 環境だけにする。triple は `x86_64-w64-windows-gnu` /
  `x86_64-w64-mingw32`。`windows` を含むが gnu とも mingw とも言わない triple は clang に
  とって MSVC なので、読み替えずに拒否する。
* driver は llvm-mingw の `x86_64-w64-mingw32-clang`。名前が target の header・CRT・linker を
  選ぶので、Windows host でも macOS / Linux からの cross build でも同じ名前で呼ぶ。
* executable は static に link する。依存するのは Windows 10 以降に最初からある DLL
  (kernel32、ws2_32、bcrypt、UCRT)だけで、配った `.exe` はそのまま動く。
* runtime は mingw に同じ意味の POSIX 呼び出しがあるもの(stat、dirent、pthread)は
  そのまま使い、意味が違うもの(socket、子 process、stack 切り替え、UTF-16 の path)は
  Win32 を直接呼ぶ。

## 却下した案

| 案 | 却下理由 |
| --- | --- |
| MSVC 環境 | link する machine に Microsoft の CRT と SDK が要る。再配布できず、macOS / Linux から cross build できない |
| 両方を受け付ける | runtime の build と検証が 2 通りになる。MSVC 製の C library と組む需要が出てから足す |
| `clang -target ...-windows-gnu` を host の clang で呼ぶ | Apple の clang は mingw の sysroot も lld も持たない |
