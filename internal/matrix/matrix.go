// Package matrix implements the Matrix appservice side of the bridge.
// It manages the appservice lifecycle, ghost users for Minecraft players,
// and bidirectional message routing.
package matrix

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/appservice"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/johannes/mmbridge/internal/config"
	"github.com/johannes/mmbridge/internal/mojang"
)

// MessageHandler is called when a real Matrix user sends a message to the bridged room.
// Parameters: sender display name, Matrix ID (e.g. @user:example.com), message body.
type MessageHandler func(sender string, mxid string, body string)

// Service manages the Matrix appservice connection.
type Service struct {
	cfg       *config.MatrixConfig
	as        *appservice.AppService
	processor *appservice.EventProcessor
	log       *slog.Logger

	// Ghost user intents, keyed by lowercase Minecraft player name.
	ghosts   map[string]*appservice.IntentAPI
	ghostsMu sync.Mutex

	// Callback for messages coming from Matrix into Minecraft.
	onMessage MessageHandler

	roomID id.RoomID
}

// NewService creates a new Matrix appservice service.
func NewService(cfg *config.MatrixConfig, logger *slog.Logger) (*Service, error) {
	reg := appservice.CreateRegistration()
	reg.ID = cfg.AppserviceID
	reg.AppToken = cfg.AppserviceToken
	reg.ServerToken = cfg.HomeserverToken
	reg.SenderLocalpart = cfg.BotUsername

	// Register the ghost user namespace.
	ghostRegex := fmt.Sprintf("@%s.*:%s", regexp.QuoteMeta(cfg.UserPrefix), regexp.QuoteMeta(cfg.Domain))
	reg.Namespaces.UserIDs.Register(regexp.MustCompile(ghostRegex), true)

	// Parse listen address into host and port.
	host, port, err := parseListenAddress(cfg.ListenAddress)
	if err != nil {
		return nil, fmt.Errorf("matrix: invalid listen_address %q: %w", cfg.ListenAddress, err)
	}

	as, err := appservice.CreateFull(appservice.CreateOpts{
		Registration:     reg,
		HomeserverDomain: cfg.Domain,
		HomeserverURL:    cfg.Homeserver,
		HostConfig: appservice.HostConfig{
			Hostname: host,
			Port:     port,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("matrix: create appservice: %w", err)
	}

	s := &Service{
		cfg:    cfg,
		as:     as,
		log:    logger,
		ghosts: make(map[string]*appservice.IntentAPI),
		roomID: id.RoomID(cfg.RoomID),
	}

	// Set up event processing.
	s.processor = appservice.NewEventProcessor(as)
	s.processor.On(event.EventMessage, s.handleMatrixMessage)

	return s, nil
}

// SetMessageHandler sets the callback invoked when a real Matrix user sends a message.
func (s *Service) SetMessageHandler(h MessageHandler) {
	s.onMessage = h
}

// Start starts the appservice HTTP listener and event processor.
// It blocks until the context is cancelled.
func (s *Service) Start(ctx context.Context) error {
	s.log.Info("starting matrix appservice",
		"listen", s.cfg.ListenAddress,
		"bot", fmt.Sprintf("@%s:%s", s.cfg.BotUsername, s.cfg.Domain),
	)

	// Ensure bot is registered and in the room.
	bot := s.as.BotIntent()
	if err := bot.EnsureRegistered(ctx); err != nil {
		s.log.Warn("bot registration (may already exist)", "error", err)
	}
	if err := bot.EnsureJoined(ctx, s.roomID); err != nil {
		s.log.Warn("bot join room (may already be joined)", "error", err)
	}

	go s.processor.Start(ctx)
	go s.as.Start()

	<-ctx.Done()
	s.as.Stop()
	s.processor.Stop()
	return ctx.Err()
}

// SendMessage sends a chat message to the Matrix room as a Minecraft player ghost.
func (s *Service) SendMessage(ctx context.Context, player, message string) error {
	intent, err := s.getOrCreateGhost(ctx, player)
	if err != nil {
		return fmt.Errorf("matrix: ghost for %s: %w", player, err)
	}

	_, err = intent.SendText(ctx, s.roomID, message)
	if err != nil {
		return fmt.Errorf("matrix: send message as %s: %w", player, err)
	}
	return nil
}

// SendEmote sends an emote (action) to the Matrix room as a Minecraft player ghost.
func (s *Service) SendEmote(ctx context.Context, player, message string) error {
	intent, err := s.getOrCreateGhost(ctx, player)
	if err != nil {
		return fmt.Errorf("matrix: ghost for %s: %w", player, err)
	}

	_, err = intent.SendMessageEvent(ctx, s.roomID, event.EventMessage, &event.MessageEventContent{
		MsgType: event.MsgEmote,
		Body:    message,
	})
	if err != nil {
		return fmt.Errorf("matrix: send emote as %s: %w", player, err)
	}
	return nil
}

// SendNotice sends a notice (system message) to the Matrix room as the bot.
func (s *Service) SendNotice(ctx context.Context, message string) error {
	_, err := s.as.BotIntent().SendNotice(ctx, s.roomID, message)
	if err != nil {
		return fmt.Errorf("matrix: send notice: %w", err)
	}
	return nil
}

// handleMatrixMessage processes incoming Matrix messages and forwards them to Minecraft.
func (s *Service) handleMatrixMessage(ctx context.Context, evt *event.Event) {
	s.log.Debug("matrix event received",
		"sender", evt.Sender,
		"room", evt.RoomID,
		"type", evt.Type.Type,
	)

	// Ignore messages from our own bot or ghost users.
	if s.isOwnUser(evt.Sender) {
		s.log.Debug("ignoring own user", "sender", evt.Sender)
		return
	}

	// Only handle messages in our bridged room.
	if evt.RoomID != s.roomID {
		s.log.Debug("ignoring wrong room", "got", evt.RoomID, "want", s.roomID)
		return
	}

	// Ensure content is parsed. If already parsed, the error is harmless.
	_ = evt.Content.ParseRaw(evt.Type)

	content := evt.Content.AsMessage()
	if content == nil {
		s.log.Debug("event content is not a message (nil)")
		return
	}

	s.log.Debug("message content", "msgtype", content.MsgType, "body", content.Body)

	if content.MsgType != event.MsgText {
		s.log.Debug("ignoring non-text message", "msgtype", content.MsgType)
		return
	}

	displayName := s.getDisplayName(ctx, evt.Sender)

	s.log.Info("matrix->mc", "sender", displayName, "body", content.Body)

	if s.onMessage != nil {
		s.onMessage(displayName, string(evt.Sender), content.Body)
	}
}

// isOwnUser checks if a user ID belongs to the bridge (bot or ghost).
func (s *Service) isOwnUser(userID id.UserID) bool {
	localpart, domain, err := userID.Parse()
	if err != nil {
		return false
	}
	if domain != s.cfg.Domain {
		return false
	}
	if localpart == s.cfg.BotUsername {
		return true
	}
	if strings.HasPrefix(localpart, s.cfg.UserPrefix) {
		return true
	}
	return false
}

// getDisplayName fetches the display name of a Matrix user.
func (s *Service) getDisplayName(ctx context.Context, userID id.UserID) string {
	resp, err := s.as.BotIntent().GetDisplayName(ctx, userID)
	if err != nil || resp.DisplayName == "" {
		localpart, _, _ := userID.Parse()
		return localpart
	}
	return resp.DisplayName
}

// getOrCreateGhost returns (and lazily provisions) an intent for a Minecraft player ghost.
func (s *Service) getOrCreateGhost(ctx context.Context, player string) (*appservice.IntentAPI, error) {
	key := strings.ToLower(player)

	s.ghostsMu.Lock()
	intent, ok := s.ghosts[key]
	s.ghostsMu.Unlock()
	if ok {
		return intent, nil
	}

	localpart := s.cfg.UserPrefix + strings.ToLower(player)
	userID := id.NewUserID(localpart, s.cfg.Domain)
	intent = s.as.Intent(userID)

	if err := intent.EnsureRegistered(ctx); err != nil {
		s.log.Debug("ghost register (may already exist)", "player", player, "error", err)
	}

	if err := intent.SetDisplayName(ctx, player); err != nil {
		s.log.Warn("ghost set display name", "player", player, "error", err)
	}

	// Upload profile pictures, if possible
	pdata, err := mojang.GetPlayer(player)
	if err == nil {
		face, err := pdata.GetFace()
		if err == nil {
			req := mautrix.ReqUploadMedia{
				ContentBytes: face,
				ContentType:  "image/png",
			}
			resp, err := intent.UploadMedia(ctx, req)
			if err != nil {
				s.log.Warn("Could not upload avatar", "player", player, "error", err)
			}
			intent.SetAvatarURL(ctx, resp.ContentURI)
		} else {
			s.log.Error("could not fetch skin data", "player", player, "error", err)
		}
	} else {
		s.log.Error("could not fetch player information", "player", player, "error", err)
	}

	if err := intent.EnsureJoined(ctx, s.roomID); err != nil {
		return nil, fmt.Errorf("ghost join room: %w", err)
	}

	s.ghostsMu.Lock()
	s.ghosts[key] = intent
	s.ghostsMu.Unlock()

	s.log.Info("provisioned ghost user", "player", player, "mxid", userID)
	return intent, nil
}

// parseListenAddress splits ":8009" or "0.0.0.0:8009" into host and port.
func parseListenAddress(addr string) (string, uint16, error) {
	// Handle ":port" form
	if strings.HasPrefix(addr, ":") {
		var port uint16
		_, err := fmt.Sscanf(addr, ":%d", &port)
		if err != nil {
			return "", 0, fmt.Errorf("parse port: %w", err)
		}
		return "0.0.0.0", port, nil
	}

	// Handle "host:port" form
	parts := strings.SplitN(addr, ":", 2)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("expected host:port, got %q", addr)
	}
	var port uint16
	_, err := fmt.Sscanf(parts[1], "%d", &port)
	if err != nil {
		return "", 0, fmt.Errorf("parse port: %w", err)
	}
	return parts[0], port, nil
}
