self:

{ config, lib, pkgs, ... }:

let
  cfg = config.services.mmbridge;

  # Generate the config YAML purely from Nix.
  # Secret values are omitted; _file fields point to LoadCredential paths.
  configFile = (pkgs.formats.yaml { }).generate "mmbridge.yaml" {
    minecraft = {
      rcon_address = cfg.minecraft.rconAddress;
      rcon_password_file = "/run/credentials/mmbridge.service/rcon_password";
      log_file = cfg.minecraft.logFile;
    };
    matrix = {
      homeserver = cfg.matrix.homeserver;
      domain = cfg.matrix.domain;
      appservice_id = cfg.matrix.appserviceId;
      as_token_file = "/run/credentials/mmbridge.service/as_token";
      hs_token_file = "/run/credentials/mmbridge.service/hs_token";
      listen_address = cfg.matrix.listenAddress;
      bot_username = cfg.matrix.botUsername;
      user_prefix = cfg.matrix.userPrefix;
      room_id = cfg.matrix.roomId;
    };
    bridge = {
      relay_join_leave = cfg.bridge.relayJoinLeave;
      relay_deaths = cfg.bridge.relayDeaths;
      relay_advancements = cfg.bridge.relayAdvancements;
      command_prefix = cfg.bridge.commandPrefix;
    };
  };
in
{
  options.services.mmbridge = {
    enable = lib.mkEnableOption "mmbridge Minecraft-Matrix chat bridge";

    package = lib.mkOption {
      type = lib.types.package;
      default = self.packages.${pkgs.system}.mmbridge;
      defaultText = lib.literalExpression "mmbridge.packages.\${system}.mmbridge";
      description = "The mmbridge package to use.";
    };

    logLevel = lib.mkOption {
      type = lib.types.enum [ "debug" "info" "warn" "error" ];
      default = "info";
      description = "Log level for the mmbridge daemon.";
    };

    # -- Minecraft ----------------------------------------------------------
    minecraft = {
      rconAddress = lib.mkOption {
        type = lib.types.str;
        default = "localhost:25575";
        description = "RCON address of the Minecraft server (host:port).";
      };

      rconPasswordFile = lib.mkOption {
        type = lib.types.path;
        description = ''
          Path to a file containing the RCON password.
        '';
      };

      logFile = lib.mkOption {
        type = lib.types.str;
        description = "Path to the Minecraft server's latest.log file.";
        example = "/var/lib/minecraft/logs/latest.log";
      };
    };

    # -- Matrix -------------------------------------------------------------
    matrix = {
      homeserver = lib.mkOption {
        type = lib.types.str;
        description = "Matrix homeserver client-server API URL.";
        example = "https://matrix.example.com";
      };

      domain = lib.mkOption {
        type = lib.types.str;
        description = "Matrix homeserver server_name / domain.";
        example = "example.com";
      };

      appserviceId = lib.mkOption {
        type = lib.types.str;
        default = "minecraft";
        description = "Appservice registration ID.";
      };

      appserviceTokenFile = lib.mkOption {
        type = lib.types.path;
        description = ''
          Path to a file containing the appservice token (as_token).
        '';
      };

      homeserverTokenFile = lib.mkOption {
        type = lib.types.path;
        description = ''
          Path to a file containing the homeserver token (hs_token).
        '';
      };

      listenAddress = lib.mkOption {
        type = lib.types.str;
        default = ":8009";
        description = "Address for the appservice HTTP listener (host:port or :port).";
      };

      botUsername = lib.mkOption {
        type = lib.types.str;
        default = "minecraft";
        description = "Localpart for the bridge bot user.";
      };

      userPrefix = lib.mkOption {
        type = lib.types.str;
        default = "mc_";
        description = "Localpart prefix for Minecraft player ghost users.";
      };

      roomId = lib.mkOption {
        type = lib.types.str;
        description = "Matrix room ID to bridge (e.g. !abcdef:example.com).";
      };
    };

    # -- Bridge behaviour ---------------------------------------------------
    bridge = {
      relayJoinLeave = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Relay Minecraft join/leave events to Matrix.";
      };

      relayDeaths = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Relay Minecraft death messages to Matrix.";
      };

      relayAdvancements = lib.mkOption {
        type = lib.types.bool;
        default = true;
        description = "Relay Minecraft advancements to Matrix.";
      };

      commandPrefix = lib.mkOption {
        type = lib.types.str;
        default = "!mc";
        description = "Command prefix for Matrix-side bridge commands.";
      };
    };
  };

  config = lib.mkIf cfg.enable {
    users.users.mmbridge = {
      isSystemUser = true;
      group = "mmbridge";
      description = "mmbridge service user";
    };
    users.groups.mmbridge = { };

    systemd.services.mmbridge = {
      description = "Minecraft-Matrix Chat Bridge";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        Type = "simple";
        User = "minecraft";
        Group = "minecraft";
        Restart = "on-failure";
        RestartSec = 10;

        ExecStart = "${lib.getExe cfg.package} -config ${configFile} -log-level ${cfg.logLevel}";

        LoadCredential = [
          "rcon_password:${cfg.minecraft.rconPasswordFile}"
          "as_token:${cfg.matrix.appserviceTokenFile}"
          "hs_token:${cfg.matrix.homeserverTokenFile}"
        ];

        # Hardening
        NoNewPrivileges = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictSUIDSGID = true;
        MemoryDenyWriteExecute = true;
        LockPersonality = true;
      };
    };
  };
}
