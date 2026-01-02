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
    # setup python env
    uv sync --locked
    source .venv/bin/activate
  '';
}
