// Package bridge ties together the Minecraft command transport, log tailer,
// and Matrix appservice to form a bidirectional chat bridge.
package bridge

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/johannes/mmbridge/internal/config"
	"github.com/johannes/mmbridge/internal/formatting"
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

	// Used only for protecting writes to FIFO
	fMu sync.Mutex
}

// New creates a new Bridge from the given configuration.
func New(cfg *config.Config, logger *slog.Logger) (*Bridge, error) {
	var rc *rcon.Client
	if cfg.Minecraft.RCONAddress != "" {
		rc = rcon.NewClient(
			cfg.Minecraft.RCONAddress,
			cfg.Minecraft.RCONPassword,
			10*time.Second,
		)
	}

	t := logtail.NewTailer(cfg.Minecraft.LogFile, 500*time.Millisecond)

	ms, err := matrix.NewService(
		&cfg.Matrix,
		logger.With("component", "matrix"),
	)
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
	// Only connect to RCON if RCON is configured.
	if b.rcon != nil {
		if err := b.connectRCON(ctx); err != nil {
			return fmt.Errorf("bridge: rcon connect: %w", err)
		}
		defer b.rcon.Close()
	}

	// Start log tailer.
	events, err := b.tailer.Tail(ctx)
	if err != nil {
		return fmt.Errorf("bridge: start log tailer: %w", err)
	}

	// Start Minecraft -> Matrix event forwarder in background.
	go b.forwardMinecraftEvents(ctx, events)

	// Start Matrix appservice.
	b.log.Info("bridge started")
	return b.matrix.Start(ctx)
}

// connectRCON connects to the Minecraft RCON server, retrying a few times.
func (b *Bridge) connectRCON(ctx context.Context) error {
	if b.rcon == nil {
		return fmt.Errorf("RCON is not configured")
	}

	var lastErr error

	for i := 0; i < 5; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		b.log.Info(
			"connecting to minecraft rcon",
			"addr", b.cfg.Minecraft.RCONAddress,
			"attempt", i+1,
		)

		if err := b.rcon.Connect(); err != nil {
			lastErr = err

			b.log.Warn(
				"rcon connect failed, retrying",
				"error", err,
				"attempt", i+1,
			)

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

// executeMinecraftCommand sends a command using RCON when configured,
// otherwise via the local Minecraft stdin FIFO.
//
// RCON may return a response, but the bridge does not currently use it.
func (b *Bridge) executeMinecraftCommand(command string) error {
	b.log.Debug("executing", "command", command)

	if b.rcon != nil {
		if _, err := b.rcon.Execute(command); err != nil {
			b.log.Warn("rcon command failed, reconnecting", "error", err)

			if reconnErr := b.rcon.Connect(); reconnErr != nil {
				return fmt.Errorf(
					"rcon command: %w (reconnect failed: %v)",
					err,
					reconnErr,
				)
			}

			if _, retryErr := b.rcon.Execute(command); retryErr != nil {
				return fmt.Errorf("rcon command retry: %w", retryErr)
			}
		}

		return nil
	}

	if b.cfg.Minecraft.FIFO == "" {
		return fmt.Errorf("no Minecraft command transport configured")
	}

	b.fMu.Lock()
	defer b.fMu.Unlock()

	f, err := os.OpenFile(b.cfg.Minecraft.FIFO, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("open minecraft fifo: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, command); err != nil {
		return fmt.Errorf("write minecraft fifo: %w", err)
	}

	return nil
}

// forwardMinecraftEvents reads events from the log tailer and sends them to Matrix.
func (b *Bridge) forwardMinecraftEvents(
	ctx context.Context,
	events <-chan *logtail.Event,
) {
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
	// Log the event
	b.log.Info(ev.String())

	// Convert Minecraft message codes
	ev.Message = formatting.StripEscapes(ev.Message)
	switch ev.Type {
	case logtail.EventChat:
		if err := b.matrix.SendMessage(ctx, ev.Player, ev.Message); err != nil {
			b.log.Error("forward chat to matrix", "error", err)
		}

	case logtail.EventJoin:
		if err := b.matrix.SetOnline(ctx, ev.Player, true); err != nil {
			b.log.Error("setting presence", "online", true, "player", ev.Player)
		}

		if !b.cfg.Bridge.RelayJoinLeave {
			return
		}

		msg := fmt.Sprintf("%s joined the game", ev.Player)
		if err := b.matrix.SendNotice(ctx, msg); err != nil {
			b.log.Error("forward join to matrix", "error", err)
		}

	case logtail.EventLeave:
		if err := b.matrix.SetOnline(ctx, ev.Player, false); err != nil {
			b.log.Error("setting presence", "online", false, "player", ev.Player)
		}

		if !b.cfg.Bridge.RelayJoinLeave {
			return
		}

		msg := fmt.Sprintf("%s left the game", ev.Player)
		if err := b.matrix.SendNotice(ctx, msg); err != nil {
			b.log.Error("forward leave to matrix", "error", err)
		}

	case logtail.EventDeath:
		if !b.cfg.Bridge.RelayDeaths {
			return
		}

		if err := b.matrix.SendNotice(ctx, ev.Message); err != nil {
			b.log.Error("forward death to matrix", "error", err)
		}

	case logtail.EventAdvancement:
		if !b.cfg.Bridge.RelayAdvancements {
			return
		}

		msg := fmt.Sprintf(
			"%s has made the advancement [%s]",
			ev.Player,
			ev.Message,
		)

		if err := b.matrix.SendNotice(ctx, msg); err != nil {
			b.log.Error("forward advancement to matrix", "error", err)
		}

	case logtail.EventServerMsg:
		if !b.cfg.Bridge.RelayServer {
			return
		}

		if err := b.matrix.SendNotice(
			ctx,
			fmt.Sprintf("[%s] %s", ev.Player, ev.Message),
		); err != nil {
			b.log.Error("forward server msg to matrix", "error", err)
		}
	}
}

// handleMatrixMessage is called when a real Matrix user sends a message.
// It forwards the message to Minecraft using /tellraw via RCON or FIFO.
func (b *Bridge) handleMatrixMessage(sender, mxid, body string) {
	// Escape all JSON strings before embedding them in tellraw.
	escaped := jsonEscape(body)
	escaped = formatting.MarkdownToMinecraft(body)
	senderEscaped := jsonEscape(sender)
	mxidEscaped := jsonEscape(mxid)

	tellraw := fmt.Sprintf(
		`tellraw @a [{"text":"[Matrix] ","color":"green","hoverEvent":{"action":"show_text","value":"%s"}},{"text":"<%s> ","color":"white","hoverEvent":{"action":"show_text","value":"%s"}},{"text":"%s","color":"white"}]`,
		mxidEscaped,
		senderEscaped,
		mxidEscaped,
		escaped,
	)

	if err := b.executeMinecraftCommand(tellraw); err != nil {
		b.log.Error("minecraft tellraw", "error", err)
	}
}

// jsonEscape escapes a string for safe embedding inside a JSON string literal.
func jsonEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return s
}
