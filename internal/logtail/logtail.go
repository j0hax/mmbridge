// Package logtail watches the Minecraft server log file for chat messages
// and other events (join/leave/death/advancement).
package logtail

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"time"
)

// EventType describes the kind of event parsed from a log line.
type EventType int

const (
	EventChat        EventType = iota // Player sent a chat message
	EventJoin                         // Player joined the server
	EventLeave                        // Player left the server
	EventDeath                        // Player died
	EventAdvancement                  // Player earned an advancement
	EventServerMsg                    // Server broadcast (e.g. /say)
)

// Event represents a parsed Minecraft server log event.
type Event struct {
	Type      EventType
	Player    string
	Message   string // chat text, death message, advancement name, etc.
	RawLine   string
	Timestamp string
}

func (e EventType) String() string {
	switch e {
	case EventChat:
		return "chat"
	case EventJoin:
		return "join"
	case EventLeave:
		return "leave"
	case EventDeath:
		return "death"
	case EventAdvancement:
		return "advancement"
	case EventServerMsg:
		return "server"
	default:
		return "unknown"
	}
}

// Compiled patterns for Minecraft log lines.
// Log format: [HH:MM:SS] [Server thread/INFO]: <message>
var (
	// Matches the log prefix and captures the timestamp and message body.
	// Optionally matches the "[Not Secure] " tag prepended to messages from
	// players who connect without chat signing (Minecraft 1.19.1+).
	reLogLine = regexp.MustCompile(`^\[(\d{2}:\d{2}:\d{2})\] \[Server thread/INFO\]:\s+(?:\[Not Secure\] )?(?:System chat: )?(.+)$`)

	// Chat: <PlayerName> message text
	reChat = regexp.MustCompile(`^<(\w+)>\s+(.+)$`)

	// Join: PlayerName joined the game
	reJoin = regexp.MustCompile(`^(\w+) joined the game$`)

	// Leave: PlayerName left the game
	reLeave = regexp.MustCompile(`^(\w+) left the game$`)

	// Advancement: PlayerName has made the advancement [Advancement Name]
	reAdvancement = regexp.MustCompile(`^(\w+) has (?:made the advancement|completed the challenge) \[(.+)\]$`)

	// Server /say: [Server] message  or  [PlayerName] message (from /say)
	reServerSay = regexp.MustCompile(`^\[(\w+)\]\s+(.+)$`)

	// Death messages are hard to enumerate fully — common patterns:
	// "PlayerName was slain by ...", "PlayerName drowned", "PlayerName fell from ...", etc.
	// We use a heuristic: any INFO line that starts with a known player word
	// and doesn't match the above patterns is treated as a death message.
	// The bridge can filter these out if needed.
)

// Common death message keywords that follow a player name.
var deathVerbs = []string{
	"was slain", "was shot", "was killed", "was fireballed", "was pummeled",
	"drowned", "blew up", "was blown up", "hit the ground", "fell",
	"was squashed", "was impaled", "was skewered",
	"burned to death", "was burnt", "tried to swim in lava",
	"suffocated", "starved to death", "was poked to death",
	"withered away", "was pricked to death", "walked into a cactus",
	"was struck by lightning", "didn't want to live",
	"experienced kinetic energy", "went off with a bang",
	"went up in flames", "walked into fire",
	"discovered the floor was lava", "was frozen to death",
	"was stung to death",
}

// ParseLine attempts to parse a single log line into an Event.
// Returns nil if the line is not a recognized event.
func ParseLine(line string) *Event {
	m := reLogLine.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	timestamp := m[1]
	body := m[2]

	// Chat message
	if cm := reChat.FindStringSubmatch(body); cm != nil {
		return &Event{
			Type:      EventChat,
			Player:    cm[1],
			Message:   cm[2],
			RawLine:   line,
			Timestamp: timestamp,
		}
	}

	// Join
	if jm := reJoin.FindStringSubmatch(body); jm != nil {
		return &Event{
			Type:      EventJoin,
			Player:    jm[1],
			RawLine:   line,
			Timestamp: timestamp,
		}
	}

	// Leave
	if lm := reLeave.FindStringSubmatch(body); lm != nil {
		return &Event{
			Type:      EventLeave,
			Player:    lm[1],
			RawLine:   line,
			Timestamp: timestamp,
		}
	}

	// Advancement
	if am := reAdvancement.FindStringSubmatch(body); am != nil {
		return &Event{
			Type:      EventAdvancement,
			Player:    am[1],
			Message:   am[2],
			RawLine:   line,
			Timestamp: timestamp,
		}
	}

	// Server /say
	if sm := reServerSay.FindStringSubmatch(body); sm != nil {
		return &Event{
			Type:      EventServerMsg,
			Player:    sm[1],
			Message:   sm[2],
			RawLine:   line,
			Timestamp: timestamp,
		}
	}

	// Death messages heuristic
	for _, verb := range deathVerbs {
		if idx := strings.Index(body, " "+verb); idx > 0 {
			player := body[:idx]
			// Validate that the "player" portion looks like a player name (single word, no spaces).
			if !strings.Contains(player, " ") && len(player) <= 16 {
				return &Event{
					Type:      EventDeath,
					Player:    player,
					Message:   body,
					RawLine:   line,
					Timestamp: timestamp,
				}
			}
		}
	}

	return nil
}

// Tailer watches a Minecraft server log file and emits events on a channel.
type Tailer struct {
	path     string
	pollRate time.Duration
}

// NewTailer creates a log tailer for the given log file path.
func NewTailer(logPath string, pollRate time.Duration) *Tailer {
	if pollRate <= 0 {
		pollRate = 500 * time.Millisecond
	}
	return &Tailer{
		path:     logPath,
		pollRate: pollRate,
	}
}

// Tail starts a goroutine that tails the log file and sends parsed events
// to the returned channel. It seeks to the end of the file on start, so
// only new lines are processed. The goroutine exits when ctx is cancelled.
func (t *Tailer) Tail(ctx context.Context) (<-chan *Event, error) {
	f, err := os.Open(t.path)
	if err != nil {
		return nil, fmt.Errorf("logtail: open %s: %w", t.path, err)
	}

	// Remember the identity of the file we opened. This lets us detect
	// rotation where the old file is renamed and a new file is created.
	fileInfo, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("logtail: stat %s: %w", t.path, err)
	}

	// Seek to end — we only care about new messages.
	if _, err := f.Seek(0, io.SeekEnd); err != nil {
		f.Close()
		return nil, fmt.Errorf("logtail: seek: %w", err)
	}

	ch := make(chan *Event, 64)

	slog.Debug("starting tail goroutine", "file", f.Name())

	go func() {
		defer func() {
			_ = f.Close()
		}()
		defer close(ch)

		reader := bufio.NewReader(f)
		ticker := time.NewTicker(t.pollRate)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return

			case <-ticker.C:
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}

					line, err := reader.ReadString('\n')

					if len(line) > 0 {
						slog.Debug("read", "line", line)
					}

					if len(line) > 0 && strings.HasSuffix(line, "\n") {
						line = strings.TrimRight(line, "\r\n")

						if ev := ParseLine(line); ev != nil {
							select {
							case ch <- ev:
							case <-ctx.Done():
								return
							}
						}
					}

					if err != nil {
						if err != io.EOF {
							break
						}

						pathInfo, err := os.Stat(t.path)
						if err != nil {
							break
						}

						// Rename/create rotation.
						if !os.SameFile(fileInfo, pathInfo) {
							slog.Info("detected log rotation", "fileInfo", fileInfo, "pathInfo", pathInfo)
							newFile, newInfo, err := reopenLog(t.path)
							if err != nil {
								break
							}

							oldFile := f

							f = newFile
							fileInfo = newInfo
							reader = bufio.NewReader(f)

							_ = oldFile.Close()

							continue
						}

						// copytruncate rotation.
						pos, err := f.Seek(0, io.SeekCurrent)
						if err == nil && pathInfo.Size() < pos {
							slog.Info("detected copytruncate log rotation", "pos", pos)
							if _, err := f.Seek(0, io.SeekStart); err == nil {
								reader.Reset(f)
							}
						}

						break
					}
				}
			}
		}
	}()

	return ch, nil
}

// reopenLog reopens the file at t.path. For a newly-created file after log
// rotation, start at the beginning so lines written before we
// notice the rotation are not skipped.
func reopenLog(path string) (*os.File, os.FileInfo, error) {
	slog.Debug("reopening log", "path", path)
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, nil, err
	}

	return f, info, nil
}
