// Package config handles loading and validating the bridge configuration.
package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// Config is the top-level configuration for the Minecraft-Matrix bridge.
type Config struct {
	// Minecraft server settings.
	Minecraft MinecraftConfig `yaml:"minecraft"`

	// Matrix appservice settings.
	Matrix MatrixConfig `yaml:"matrix"`

	// Bridge behaviour settings.
	Bridge BridgeConfig `yaml:"bridge"`
}

// MinecraftConfig holds Minecraft server connection details.
type MinecraftConfig struct {
	// RCON address in host:port form (e.g. "localhost:25575").
	RCONAddress  string `yaml:"rcon_address"`
	RCONPassword string `yaml:"rcon_password"`

	// Alternative: read the RCON password from a file at startup.
	RCONPasswordFile string `yaml:"rcon_password_file"`

	// Path to the server's latest.log file for tailing chat.
	LogFile string `yaml:"log_file"`
}

// MatrixConfig holds Matrix homeserver and appservice settings.
type MatrixConfig struct {
	// Homeserver URL (e.g. "https://matrix.example.com").
	Homeserver string `yaml:"homeserver"`

	// The domain part of the homeserver (e.g. "example.com").
	Domain string `yaml:"domain"`

	// Appservice registration details.
	AppserviceID    string `yaml:"appservice_id"`
	AppserviceToken string `yaml:"as_token"`
	HomeserverToken string `yaml:"hs_token"`

	// Alternative: read tokens from files at startup.
	AppserviceTokenFile string `yaml:"as_token_file"`
	HomeserverTokenFile string `yaml:"hs_token_file"`

	// Address to listen on for incoming homeserver requests (e.g. ":8009").
	ListenAddress string `yaml:"listen_address"`

	// Bot user localpart (e.g. "minecraft"). Full MXID will be @minecraft:domain.
	BotUsername string `yaml:"bot_username"`

	// Namespace prefix for ghost users (e.g. "mc_"). Ghosts will be @mc_<playername>:domain.
	UserPrefix string `yaml:"user_prefix"`

	// The Matrix room ID or alias to bridge to.
	RoomID string `yaml:"room_id"`
}

// BridgeConfig holds behaviour toggles.
type BridgeConfig struct {
	// Whether to relay join/leave events.
	RelayJoinLeave bool `yaml:"relay_join_leave"`

	// Whether to relay death messages.
	RelayDeaths bool `yaml:"relay_deaths"`

	// Whether to relay advancements.
	RelayAdvancements bool `yaml:"relay_advancements"`

	// Command prefix for Matrix->MC commands (default: "!mc").
	CommandPrefix string `yaml:"command_prefix"`
}

// Load reads and parses the configuration from the given YAML file path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	cfg := &Config{
		// Defaults
		Bridge: BridgeConfig{
			RelayJoinLeave:    true,
			RelayDeaths:       true,
			RelayAdvancements: true,
			CommandPrefix:     "!mc",
		},
		Matrix: MatrixConfig{
			ListenAddress: ":8009",
			BotUsername:   "minecraft",
			UserPrefix:    "mc_",
			AppserviceID:  "minecraft",
		},
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse: %w", err)
	}

	// Resolve _file fields: read secret from file if the inline value is empty.
	if err := cfg.resolveSecretFiles(); err != nil {
		return nil, err
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// resolveSecretFiles reads secrets from files when *_file options are set.
func (c *Config) resolveSecretFiles() error {
	var err error
	if c.Minecraft.RCONPassword == "" && c.Minecraft.RCONPasswordFile != "" {
		c.Minecraft.RCONPassword, err = readSecretFile(c.Minecraft.RCONPasswordFile)
		if err != nil {
			return fmt.Errorf("config: minecraft.rcon_password_file: %w", err)
		}
	}
	if c.Matrix.AppserviceToken == "" && c.Matrix.AppserviceTokenFile != "" {
		c.Matrix.AppserviceToken, err = readSecretFile(c.Matrix.AppserviceTokenFile)
		if err != nil {
			return fmt.Errorf("config: matrix.as_token_file: %w", err)
		}
	}
	if c.Matrix.HomeserverToken == "" && c.Matrix.HomeserverTokenFile != "" {
		c.Matrix.HomeserverToken, err = readSecretFile(c.Matrix.HomeserverTokenFile)
		if err != nil {
			return fmt.Errorf("config: matrix.hs_token_file: %w", err)
		}
	}
	return nil
}

// readSecretFile reads a file and returns its contents with surrounding whitespace trimmed.
func readSecretFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func (c *Config) validate() error {
	if c.Minecraft.RCONAddress == "" {
		return fmt.Errorf("config: minecraft.rcon_address is required")
	}
	if c.Minecraft.RCONPassword == "" {
		return fmt.Errorf("config: minecraft.rcon_password is required")
	}
	if c.Minecraft.LogFile == "" {
		return fmt.Errorf("config: minecraft.log_file is required")
	}
	if c.Matrix.Homeserver == "" {
		return fmt.Errorf("config: matrix.homeserver is required")
	}
	if c.Matrix.Domain == "" {
		return fmt.Errorf("config: matrix.domain is required")
	}
	if c.Matrix.AppserviceToken == "" {
		return fmt.Errorf("config: matrix.as_token is required")
	}
	if c.Matrix.HomeserverToken == "" {
		return fmt.Errorf("config: matrix.hs_token is required")
	}
	if c.Matrix.RoomID == "" {
		return fmt.Errorf("config: matrix.room_id is required")
	}
	return nil
}
