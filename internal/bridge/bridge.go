// Package bridge ties together the Minecraft RCON client, log tailer,
// and Matrix appservice to form a bidirectional chat bridge.
package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/johannes/mmbridge/internal/config"
	"github.com/johannes/mmbridge/internal/logtail"
	"github.com/johannes/mmbridge/internal/matrix"
	"github.com/johannes/mmbridge/internal/rcon"
)

// Bridge is the main coordinator that routes messages between Minecraft and Matrix.
type Bridge struct {
	cfg    *config.Config
	rcon   *rcon.Client
	tailer *logtail.Tailer
	matrix *matrix.Service
	log    *slog.Logger
}

// New creates a new Bridge from the given configuration.
func New(cfg *config.Config, logger *slog.Logger) (*Bridge, error) {
	rc := rcon.NewClient(cfg.Minecraft.RCONAddress, cfg.Minecraft.RCONPassword, 10*time.Second)

	t := logtail.NewTailer(cfg.Minecraft.LogFile, 500*time.Millisecond)

	ms, err := matrix.NewService(&cfg.Matrix, logger.With("component", "matrix"))
	if err != nil {
		return nil, fmt.Errorf("bridge: init matrix: %w", err)
	}

	b := &Bridge{
		cfg:    cfg,
		rcon:   rc,
		tailer: t,
		matrix: ms,
		log:    logger,
	}

	// Wire Matrix -> Minecraft direction.
	ms.SetMessageHandler(b.handleMatrixMessage)

	return b, nil
}

// Run starts the bridge and blocks until the context is cancelled.
func (b *Bridge) Run(ctx context.Context) error {
	// Connect to RCON with retry.
	if err := b.connectRCON(ctx); err != nil {
		return fmt.Errorf("bridge: rcon connect: %w", err)
	}
	defer b.rcon.Close()

	// Start log tailer.
	events, err := b.tailer.Tail(ctx)
	if err != nil {
		return fmt.Errorf("bridge: start log tailer: %w", err)
	}

	// Start Minecraft -> Matrix event forwarder in background.
	go b.forwardMinecraftEvents(ctx, events)

	// Start Matrix appservice (blocks until ctx done).
	b.log.Info("bridge started")
	return b.matrix.Start(ctx)
}

// connectRCON connects to the Minecraft RCON server, retrying a few times.
func (b *Bridge) connectRCON(ctx context.Context) error {
	var lastErr error
	for i := 0; i < 5; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		b.log.Info("connecting to minecraft rcon", "addr", b.cfg.Minecraft.RCONAddress, "attempt", i+1)
		if err := b.rcon.Connect(); err != nil {
			lastErr = err
			b.log.Warn("rcon connect failed, retrying", "error", err, "attempt", i+1)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(i+1) * 2 * time.Second):
			}
			continue
		}
		b.log.Info("rcon connected")
		return nil
	}
	return fmt.Errorf("after 5 attempts: %w", lastErr)
}

// forwardMinecraftEvents reads events from the log tailer and sends them to Matrix.
func (b *Bridge) forwardMinecraftEvents(ctx context.Context, events <-chan *logtail.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			b.handleMinecraftEvent(ctx, ev)
		}
	}
}

// handleMinecraftEvent processes a single Minecraft log event and sends it to Matrix.
func (b *Bridge) handleMinecraftEvent(ctx context.Context, ev *logtail.Event) {
	switch ev.Type {
	case logtail.EventChat:
		b.log.Debug("mc chat", "player", ev.Player, "message", ev.Message)
		if err := b.matrix.SendMessage(ctx, ev.Player, ev.Message); err != nil {
			b.log.Error("forward chat to matrix", "error", err)
		}

	case logtail.EventJoin:
		if !b.cfg.Bridge.RelayJoinLeave {
			return
		}
		msg := fmt.Sprintf("%s joined the game", ev.Player)
		b.log.Debug("mc join", "player", ev.Player)
		if err := b.matrix.SendNotice(ctx, msg); err != nil {
			b.log.Error("forward join to matrix", "error", err)
		}

	case logtail.EventLeave:
		if !b.cfg.Bridge.RelayJoinLeave {
			return
		}
		msg := fmt.Sprintf("%s left the game", ev.Player)
		b.log.Debug("mc leave", "player", ev.Player)
		if err := b.matrix.SendNotice(ctx, msg); err != nil {
			b.log.Error("forward leave to matrix", "error", err)
		}

	case logtail.EventDeath:
		if !b.cfg.Bridge.RelayDeaths {
			return
		}
		b.log.Debug("mc death", "player", ev.Player, "message", ev.Message)
		if err := b.matrix.SendNotice(ctx, ev.Message); err != nil {
			b.log.Error("forward death to matrix", "error", err)
		}

	case logtail.EventAdvancement:
		if !b.cfg.Bridge.RelayAdvancements {
			return
		}
		msg := fmt.Sprintf("%s has made the advancement [%s]", ev.Player, ev.Message)
		b.log.Debug("mc advancement", "player", ev.Player, "advancement", ev.Message)
		if err := b.matrix.SendNotice(ctx, msg); err != nil {
			b.log.Error("forward advancement to matrix", "error", err)
		}

	case logtail.EventServerMsg:
		b.log.Debug("mc server msg", "sender", ev.Player, "message", ev.Message)
		if err := b.matrix.SendNotice(ctx, fmt.Sprintf("[%s] %s", ev.Player, ev.Message)); err != nil {
			b.log.Error("forward server msg to matrix", "error", err)
		}
	}
}

// handleMatrixMessage is called when a real Matrix user sends a message.
// It forwards the message to the Minecraft server via RCON /tellraw.
func (b *Bridge) handleMatrixMessage(sender, mxid, body string) {
	// Check for command prefix.
	if strings.HasPrefix(body, b.cfg.Bridge.CommandPrefix+" ") {
		cmd := strings.TrimPrefix(body, b.cfg.Bridge.CommandPrefix+" ")
		b.handleCommand(sender, cmd)
		return
	}

	// Forward as a chat message using /tellraw for formatted display.
	// We escape all JSON strings to avoid injection.
	escaped := jsonEscape(body)
	senderEscaped := jsonEscape(sender)
	mxidEscaped := jsonEscape(mxid)

	tellraw := fmt.Sprintf(
		`tellraw @a [{"text":"[Matrix] ","color":"dark_green","hoverEvent":{"action":"show_text","value":"%s"}},{"text":"<%s> ","color":"white","hoverEvent":{"action":"show_text","value":"%s"}},{"text":"%s","color":"white"}]`,
		mxidEscaped, senderEscaped, mxidEscaped, escaped,
	)

	b.log.Debug("matrix->mc", "sender", sender, "message", body)

	resp, err := b.rcon.Execute(tellraw)
	if err != nil {
		b.log.Error("rcon tellraw", "error", err)
		// Try to reconnect.
		if reconnErr := b.rcon.Connect(); reconnErr != nil {
			b.log.Error("rcon reconnect failed", "error", reconnErr)
		} else {
			// Retry once.
			if _, retryErr := b.rcon.Execute(tellraw); retryErr != nil {
				b.log.Error("rcon tellraw retry", "error", retryErr)
			}
		}
		return
	}
	if resp != "" {
		b.log.Debug("rcon response", "response", resp)
	}
}

// handleCommand handles bridge commands from Matrix.
func (b *Bridge) handleCommand(sender, cmd string) {
	switch {
	case cmd == "list" || cmd == "online":
		resp, err := b.rcon.Execute("list")
		if err != nil {
			b.log.Error("rcon list", "error", err)
			return
		}
		ctx := context.Background()
		if err := b.matrix.SendNotice(ctx, resp); err != nil {
			b.log.Error("send list response", "error", err)
		}

	default:
		b.log.Info("unknown command", "sender", sender, "command", cmd)
		ctx := context.Background()
		msg := fmt.Sprintf("Unknown command: %s\nAvailable: %s list", cmd, b.cfg.Bridge.CommandPrefix)
		if err := b.matrix.SendNotice(ctx, msg); err != nil {
			b.log.Error("send error response", "error", err)
		}
	}
}

// jsonEscape escapes a string for safe embedding inside a JSON string literal.
func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
