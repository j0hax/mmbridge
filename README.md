# mmbridge

A daemon that bridges chat between a vanilla Minecraft Java Edition server and a [Matrix](https://matrix.org/) chat room, using the [Matrix Application Service API](https://spec.matrix.org/latest/application-service-api/).

## How it works

**Minecraft to Matrix:**

- Tails the server's `latest.log` to detect chat messages, join/leave, death, and advancement events
- Chat messages appear in the Matrix room via ghost users (e.g. player `Steve` sends as `@mc_steve:example.com`)
- Join/leave, death, and advancement events are sent as bot notices

**Matrix to Minecraft:**

- Receives events from the homeserver via the appservice HTTP listener
- Forwards messages to the Minecraft server using RCON `tellraw`, displayed as `[Matrix] <username> message`
- Hovering over the username in-game shows the sender's full Matrix ID
- Supports `!mc list` to query online players from Matrix

## Requirements

- A Minecraft Java Edition server with RCON enabled
- A Matrix homeserver that supports application services (e.g. Synapse, Conduit, Dendrite)

## Installation

### Nix flake (NixOS)

Add mmbridge as a flake input and import the module:

```nix
# flake.nix
{
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    mmbridge = {
      url = "github:j0hax/mmbridge";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { nixpkgs, mmbridge, ... }: {
    nixosConfigurations.myserver = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ./configuration.nix
        mmbridge.nixosModules.default
      ];
    };
  };
}
```

Then configure the service:

```nix
# configuration.nix
{
  services.mmbridge = {
    enable = true;
    matrix = {
      homeserver = "https://matrix.example.com";
      domain = "example.com";
      appserviceTokenFile = "/run/secrets/mmbridge-as-token";
      homeserverTokenFile = "/run/secrets/mmbridge-hs-token";
      roomId = "!yourRoomId:example.com";
    };
  };
}
```

Secrets are loaded via systemd `LoadCredential` and never enter the Nix store. Point the `*File` options at files managed by your preferred secrets tool (sops-nix, agenix, etc.).

### From source

```sh
go build ./cmd/mmbridge/
```

## Manual setup

1. **Enable RCON** on your Minecraft server in `server.properties` *or* configure a Pipe to `stdin` (default on NixOS)

   ```properties
   enable-rcon=true
   rcon.port=25575
   rcon.password=your-secret-password
   ```

2. **Copy and edit the config:**

   ```sh
   cp config.example.yaml config.yaml
   # edit config.yaml with your values
   ```

3. **Generate the appservice registration** and register it with your homeserver:

   ```sh
   ./mmbridge -generate-reg -config config.yaml > registration.yaml
   ```

   Add the path to `registration.yaml` to your homeserver's `app_service_config_files` list, then restart the homeserver.

4. **Run the bridge:**

   ```sh
   ./mmbridge -config config.yaml
   ```

   Use `-log-level debug` for verbose output.

## Configuration

See [`config.example.yaml`](config.example.yaml) for all available options with comments.

Secrets can be provided inline in the YAML (`rcon_password`, `as_token`, `hs_token`) or read from files at startup (`rcon_password_file`, `as_token_file`, `hs_token_file`). The file variants take precedence when the inline value is empty.

## Matrix commands

From the bridged Matrix room:

| Command    | Description                       |
|------------|-----------------------------------|
| `!mc list` | Show players currently online     |

The command prefix is configurable (default: `!mc`).

## License

AGPLv3

### Note

This project was bootstrapped with the assistance of AI and reviewed/extended by humans.
