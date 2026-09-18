// mmbridge is a daemon that bridges chat between a Minecraft Java Edition
// server and a Matrix chat room using the Matrix Application Service API.
//
// Usage:
//
//	mmbridge -config config.yaml                  # run the bridge
//	mmbridge -config config.yaml -generate-reg    # generate appservice registration YAML
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"gopkg.in/yaml.v3"

	"github.com/johannes/mmbridge/internal/bridge"
	"github.com/johannes/mmbridge/internal/config"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to bridge configuration file")
	generateReg := flag.Bool("generate-reg", false, "generate Matrix appservice registration YAML and exit")
	logLevel := flag.String("log-level", "info", "log level (debug, info, warn, error)")
	flag.Parse()

	// Set up structured logging.
	var level slog.Level
	switch *logLevel {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		fmt.Fprintf(os.Stderr, "unknown log level %q, using info\n", *logLevel)
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	// Load configuration.
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Generate registration YAML if requested.
	if *generateReg {
		if err := generateRegistration(cfg); err != nil {
			logger.Error("failed to generate registration", "error", err)
			os.Exit(1)
		}
		return
	}

	// Set up signal handling for graceful shutdown.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("received signal, shutting down", "signal", sig)
		cancel()
	}()

	// Create and run the bridge.
	b, err := bridge.New(cfg, logger)
	if err != nil {
		logger.Error("failed to create bridge", "error", err)
		os.Exit(1)
	}

	logger.Info("starting mmbridge",
		"minecraft_rcon", cfg.Minecraft.RCONAddress,
		"matrix_homeserver", cfg.Matrix.Homeserver,
		"matrix_room", cfg.Matrix.RoomID,
	)

	if err := b.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("bridge exited with error", "error", err)
		os.Exit(1)
	}

	logger.Info("mmbridge stopped")
}

// registrationYAML is the appservice registration document structure.
type registrationYAML struct {
	ID              string     `yaml:"id"`
	URL             string     `yaml:"url"`
	ASToken         string     `yaml:"as_token"`
	HSToken         string     `yaml:"hs_token"`
	SenderLocalpart string     `yaml:"sender_localpart"`
	RateLimited     bool       `yaml:"rate_limited"`
	Namespaces      namespaces `yaml:"namespaces"`
}

type namespaces struct {
	Users  []namespace `yaml:"users"`
	Rooms  []namespace `yaml:"rooms,omitempty"`
	Aliases []namespace `yaml:"aliases,omitempty"`
}

type namespace struct {
	Exclusive bool   `yaml:"exclusive"`
	Regex     string `yaml:"regex"`
}

// generateRegistration writes a Matrix appservice registration YAML to stdout.
func generateRegistration(cfg *config.Config) error {
	ghostRegex := fmt.Sprintf("@%s.*:%s",
		regexp.QuoteMeta(cfg.Matrix.UserPrefix),
		regexp.QuoteMeta(cfg.Matrix.Domain),
	)

	reg := registrationYAML{
		ID:              cfg.Matrix.AppserviceID,
		URL:             fmt.Sprintf("http://localhost%s", cfg.Matrix.ListenAddress),
		ASToken:         cfg.Matrix.AppserviceToken,
		HSToken:         cfg.Matrix.HomeserverToken,
		SenderLocalpart: cfg.Matrix.BotUsername,
		RateLimited:     false,
		Namespaces: namespaces{
			Users: []namespace{
				{
					Exclusive: true,
					Regex:     ghostRegex,
				},
			},
		},
	}

	out, err := yaml.Marshal(reg)
	if err != nil {
		return fmt.Errorf("marshal registration: %w", err)
	}

	fmt.Print("# Matrix appservice registration for mmbridge\n")
	fmt.Print("# Place this file where your homeserver can read it and add it to\n")
	fmt.Print("# the homeserver's app_service_config_files list.\n\n")
	fmt.Print(string(out))

	return nil
}
