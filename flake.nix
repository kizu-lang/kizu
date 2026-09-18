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
    { self, nixpkgs, nixpkgs-llvm, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
        llvm = import nixpkgs-llvm { inherit system; };
      in
      {
        # `nix build` / `nix run` produce the same <prefix>/bin + <prefix>/lib
        # layout a release tarball has, and the binary names its revision
        # (`kizu version`). The nix sandbox strips VCS metadata, so the
        # revision is passed in from the flake instead.
        packages.default = pkgs.buildGo122Module {
          pname = "kizu";
          version = self.shortRev or self.dirtyShortRev or "devel";
          src = ./.;
          vendorHash = null;
          subPackages = [ "cmd/kizu" ];
          # Conformance tests link and run programs, which needs a writable
          # HOME and a system linker the build sandbox does not have. Testing
          # is CI's job; this derivation only packages the binary.
          doCheck = false;
          ldflags = [ "-X main.version=${self.shortRev or self.dirtyShortRev or "devel"}" ];
          postInstall = ''
            mkdir -p $out/lib
            cp -R lib/kizu $out/lib/kizu
          '';
        };

        devShells.default = (pkgs.mkShell.override { stdenv = llvm.llvmPackages_21.stdenv; }) {
          packages = [
            pkgs.go_1_22
            pkgs.golangci-lint
            pkgs.just
            pkgs.nodejs
            pkgs.pre-commit
            pkgs.wasmtime
          ];

          # `kizu` reads std from the library tree next to the binary it was
          # installed with. A development build has no such tree next to it, so
          # point at the one in the checkout. `go test` does not need this: its
          # TestMain sets the same override.
          shellHook = ''
            export KIZU_LIB_DIR="$PWD/lib/kizu"
          '';
        };
      }
    );
}
