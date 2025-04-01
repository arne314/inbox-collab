{
  pkgs ? import <nixpkgs> {
    config = {
      permittedInsecurePackages = [ "olm-3.2.16" ];
    };
  },
}:

pkgs.mkShell {
  packages = with pkgs; [
    go
    ollama
    olm
    sqlc
    uv
  ];

  shellHook = ''
    uv sync
    source .venv/bin/activate
  '';
}
