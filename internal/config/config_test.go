package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	return writeTempFile(t, "config.yaml", content)
}

func TestLoadAppliesDefaults(t *testing.T) {
	path := writeConfig(t, `
minecraft:
  fifo: /run/minecraft.stdin
  log_file: /var/log/minecraft/latest.log
matrix:
  homeserver: https://matrix.example.com
  domain: example.com
  as_token: as-secret
  hs_token: hs-secret
  room_id: "!room:example.com"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Matrix.ListenAddress != ":8009" {
		t.Errorf("ListenAddress = %q, want %q", cfg.Matrix.ListenAddress, ":8009")
	}
	if cfg.Matrix.BotUsername != "minecraft" {
		t.Errorf("BotUsername = %q, want %q", cfg.Matrix.BotUsername, "minecraft")
	}
	if cfg.Matrix.UserPrefix != "mc_" {
		t.Errorf("UserPrefix = %q, want %q", cfg.Matrix.UserPrefix, "mc_")
	}
	if cfg.Matrix.AppserviceID != "minecraft" {
		t.Errorf("AppserviceID = %q, want %q", cfg.Matrix.AppserviceID, "minecraft")
	}
	if !cfg.Bridge.RelayJoinLeave || !cfg.Bridge.RelayDeaths || !cfg.Bridge.RelayAdvancements {
		t.Errorf("expected relay defaults to be enabled, got %+v", cfg.Bridge)
	}
}

func TestLoadSecretFileFallback(t *testing.T) {
	rconPassword := writeTempFile(t, "rcon", "  rcon-from-file\n")
	asToken := writeTempFile(t, "as", "\tas-from-file\n")
	hsToken := writeTempFile(t, "hs", "hs-from-file\n")

	path := writeConfig(t, `
minecraft:
  rcon_address: localhost:25575
  rcon_password: ""
  rcon_password_file: `+rconPassword+`
  log_file: /var/log/minecraft/latest.log
matrix:
  homeserver: https://matrix.example.com
  domain: example.com
  as_token: ""
  as_token_file: `+asToken+`
  hs_token: ""
  hs_token_file: `+hsToken+`
  room_id: "!room:example.com"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Minecraft.RCONPassword != "rcon-from-file" {
		t.Errorf("RCONPassword = %q, want %q", cfg.Minecraft.RCONPassword, "rcon-from-file")
	}
	if cfg.Matrix.AppserviceToken != "as-from-file" {
		t.Errorf("AppserviceToken = %q, want %q", cfg.Matrix.AppserviceToken, "as-from-file")
	}
	if cfg.Matrix.HomeserverToken != "hs-from-file" {
		t.Errorf("HomeserverToken = %q, want %q", cfg.Matrix.HomeserverToken, "hs-from-file")
	}
}

func TestLoadInlineSecretsWinOverFiles(t *testing.T) {
	rconPassword := writeTempFile(t, "rcon", "rcon-from-file")
	asToken := writeTempFile(t, "as", "as-from-file")
	hsToken := writeTempFile(t, "hs", "hs-from-file")

	path := writeConfig(t, `
minecraft:
  rcon_address: localhost:25575
  rcon_password: rcon-inline
  rcon_password_file: `+rconPassword+`
  log_file: /var/log/minecraft/latest.log
matrix:
  homeserver: https://matrix.example.com
  domain: example.com
  as_token: as-inline
  as_token_file: `+asToken+`
  hs_token: hs-inline
  hs_token_file: `+hsToken+`
  room_id: "!room:example.com"
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Minecraft.RCONPassword != "rcon-inline" {
		t.Errorf("RCONPassword = %q, want inline value", cfg.Minecraft.RCONPassword)
	}
	if cfg.Matrix.AppserviceToken != "as-inline" {
		t.Errorf("AppserviceToken = %q, want inline value", cfg.Matrix.AppserviceToken)
	}
	if cfg.Matrix.HomeserverToken != "hs-inline" {
		t.Errorf("HomeserverToken = %q, want inline value", cfg.Matrix.HomeserverToken)
	}
}

func TestLoadMissingSecretFile(t *testing.T) {
	path := writeConfig(t, `
minecraft:
  rcon_address: localhost:25575
  rcon_password_file: /does/not/exist
  log_file: /var/log/minecraft/latest.log
matrix:
  homeserver: https://matrix.example.com
  domain: example.com
  as_token: as-secret
  hs_token: hs-secret
  room_id: "!room:example.com"
`)

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want error for missing secret file")
	}
	if !strings.Contains(err.Error(), "rcon_password_file") {
		t.Fatalf("Load() error = %q, want mention of rcon_password_file", err)
	}
}

func TestValidate(t *testing.T) {
	valid := Config{
		Minecraft: MinecraftConfig{
			FIFO:    "/run/minecraft.stdin",
			LogFile: "/var/log/minecraft/latest.log",
		},
		Matrix: MatrixConfig{
			Homeserver:      "https://matrix.example.com",
			Domain:          "example.com",
			AppserviceToken: "as-secret",
			HomeserverToken: "hs-secret",
			RoomID:          "!room:example.com",
		},
	}

	tests := []struct {
		name    string
		mutate  func(*Config)
		wantErr string
	}{
		{name: "valid", mutate: func(*Config) {}},
		{
			name: "missing transport",
			mutate: func(c *Config) {
				c.Minecraft.FIFO = ""
			},
			wantErr: "minecraft.fifo or minecraft.rcon_address",
		},
		{
			name: "rcon without password",
			mutate: func(c *Config) {
				c.Minecraft.FIFO = ""
				c.Minecraft.RCONAddress = "localhost:25575"
			},
			wantErr: "minecraft.rcon_password",
		},
		{
			name: "missing log file",
			mutate: func(c *Config) {
				c.Minecraft.LogFile = ""
			},
			wantErr: "minecraft.log_file",
		},
		{
			name: "missing homeserver",
			mutate: func(c *Config) {
				c.Matrix.Homeserver = ""
			},
			wantErr: "matrix.homeserver",
		},
		{
			name: "missing domain",
			mutate: func(c *Config) {
				c.Matrix.Domain = ""
			},
			wantErr: "matrix.domain",
		},
		{
			name: "missing appservice token",
			mutate: func(c *Config) {
				c.Matrix.AppserviceToken = ""
			},
			wantErr: "matrix.as_token",
		},
		{
			name: "missing homeserver token",
			mutate: func(c *Config) {
				c.Matrix.HomeserverToken = ""
			},
			wantErr: "matrix.hs_token",
		},
		{
			name: "missing room id",
			mutate: func(c *Config) {
				c.Matrix.RoomID = ""
			},
			wantErr: "matrix.room_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := valid
			tt.mutate(&cfg)

			err := cfg.validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validate() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validate() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validate() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
