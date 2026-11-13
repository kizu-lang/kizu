{
  description = "Kizu development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.05";
    # The clang the shell builds native programs with comes from a newer
    # release than the rest. 24.05's is LLVM 16, whose arm64 backend writes no
    # stack probe, which the coroutine guards rely on, and whose code for the
    # same IR runs over a tenth slower in places than LLVM 21's -- the LLVM
    # rustc carries, which is what the benchmarks against Rust compare with.
    nixpkgs-llvm.url = "github:NixOS/nixpkgs/nixos-26.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    { nixpkgs, nixpkgs-llvm, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        llvm = import nixpkgs-llvm { inherit system; };
      in
      {
        devShells.default = (pkgs.mkShell.override { stdenv = llvm.llvmPackages_21.stdenv; }) {
          packages = [
            pkgs.just
            pkgs.nodejs
            pkgs.pre-commit
            pkgs.wasmtime
          ];

          # `kizu` reads std from the library tree next to the binary it was
          # installed with. A development build has no such tree next to it, so
          # point at the one in the checkout.
          shellHook = ''
            export KIZU_LIB_DIR="$PWD/lib/kizu"
          '';
        };
      }
    );
}
