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
    olm
  ];
}
