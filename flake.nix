{
  description = "inbox-collab flake";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-parts.url = "github:hercules-ci/flake-parts";
  };

  outputs =
    inputs@{
      self,
      nixpkgs,
      flake-parts,
      ...
    }:
    flake-parts.lib.mkFlake { inherit inputs; } {
      systems = [
        "x86_64-linux"
        "aarch64-linux"
      ];

      perSystem =
        { pkgs, system, ... }:
        let
          deps = [ pkgs.olm ];
          app = pkgs.buildGoModule {
            name = "inbox-collab";
            src = ./.;
            env.CGO_ENABLED = 1;
            buildInputs = deps;
            nativeBuildInputs = deps;
            vendorHash = "sha256-Oi1FtTwI6a39YVaMZm9cNafW8QqP6LhgtQOnMb7iges=";
          };

          lintGo = pkgs.writeShellApplication {
            name = "lint-go";
            runtimeInputs =
              with pkgs;
              [
                sqlc
                go-tools
                go
              ]
              ++ deps;
            text = ''
              set -e
              export CGO_CFLAGS="-I${pkgs.olm}/include"
              staticcheck -f stylish ./...
              sqlc diff
              gofmt -e -d .
            '';
          };

          lintPython = pkgs.writeShellScriptBin "lint-python" ''
            set -e
            ${pkgs.ruff}/bin/ruff check .
            ${pkgs.ruff}/bin/ruff format --diff --check .
          '';
        in
        {
          _module.args.pkgs = import self.inputs.nixpkgs {
            inherit system;
            config.permittedInsecurePackages = [ "olm-3.2.16" ];
          };

          devShells.default = pkgs.mkShell {
            packages =
              with pkgs;
              [
                go
                go-tools
                just
                ollama
                sqlc
                uv
              ]
              ++ deps;
            shellHook = ''
              set -a
              if [ -f .env ]; then
                  source .env
              fi
              CGO_ENABLED=1
              uv sync --locked
              source .venv/bin/activate
            '';
          };

          packages = {
            default = app;
            lint-go = lintGo;
            lint-python = lintPython;
          };
        };
    };
}
