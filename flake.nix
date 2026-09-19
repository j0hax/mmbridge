{
  description = "mmbridge - Minecraft to Matrix chat bridge daemon";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages = {
          mmbridge = pkgs.buildGoModule {
            pname = "mmbridge";
            version = "0.1.0";
            src = ./.;
            vendorHash = "sha256-G8Q/mRPpoyZs4Vm0ZUNEGKwr7S9RY9Tj2xhMf/SRcKc=";
            subPackages = [ "cmd/mmbridge" ];
            meta = {
              description = "Minecraft to Matrix chat bridge using RCON and the Matrix Application Service API";
              mainProgram = "mmbridge";
            };
          };
          default = self.packages.${system}.mmbridge;
        };

        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
          ];
        };
      }
    )
    // {
      nixosModules = {
        mmbridge = import ./module.nix self;
        default = self.nixosModules.mmbridge;
      };
    };
}
