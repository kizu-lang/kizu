{
  description = "Kizu development environment";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-24.05";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    { nixpkgs, flake-utils, ... }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = import nixpkgs { inherit system; };
      in
      {
        devShells.default = pkgs.mkShell {
          packages = [
            pkgs.go_1_22
            pkgs.golangci-lint
            pkgs.just
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
