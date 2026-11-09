package logtail

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseLine(t *testing.T) {
	tests := []struct {
		name        string
		line        string
		wantNil     bool
		wantType    EventType
		wantPlayer  string
		wantMessage string
	}{
		{
			name:        "chat",
			line:        `[12:34:56] [Server thread/INFO]: <Steve> Hello world`,
			wantType:    EventChat,
			wantPlayer:  "Steve",
			wantMessage: "Hello world",
		},
		{
			name:        "chat without signed messages",
			line:        `[12:34:56] [Server thread/INFO]: [Not Secure] <Alex> hi there`,
			wantType:    EventChat,
			wantPlayer:  "Alex",
			wantMessage: "hi there",
		},
		{
			name:        "system chat",
			line:        `[12:34:56] [Server thread/INFO]: System chat: <Alex> hello`,
			wantType:    EventChat,
			wantPlayer:  "Alex",
			wantMessage: "hello",
		},
		{
			name:       "join",
			line:       `[12:34:56] [Server thread/INFO]: Steve joined the game`,
			wantType:   EventJoin,
			wantPlayer: "Steve",
		},
		{
			name:       "leave",
			line:       `[12:34:56] [Server thread/INFO]: Steve left the game`,
			wantType:   EventLeave,
			wantPlayer: "Steve",
		},
		{
			name:        "advancement",
			line:        `[12:34:56] [Server thread/INFO]: Steve has made the advancement [Stone Age]`,
			wantType:    EventAdvancement,
			wantPlayer:  "Steve",
			wantMessage: "Stone Age",
		},
		{
			name:        "challenge",
			line:        `[12:34:56] [Server thread/INFO]: Steve has completed the challenge [Monsters Hunted]`,
			wantType:    EventAdvancement,
			wantPlayer:  "Steve",
			wantMessage: "Monsters Hunted",
		},
		{
			name:        "server say",
			line:        `[12:34:56] [Server thread/INFO]: [Server] Server restarting soon`,
			wantType:    EventServerMsg,
			wantPlayer:  "Server",
			wantMessage: "Server restarting soon",
		},
		{
			name:        "death heuristic",
			line:        `[12:34:56] [Server thread/INFO]: Steve was slain by Zombie`,
			wantType:    EventDeath,
			wantPlayer:  "Steve",
			wantMessage: "Steve was slain by Zombie",
		},
		{
			name:    "unrelated server output",
			line:    `[12:34:56] [Server thread/INFO]: Starting minecraft server version 1.21`,
			wantNil: true,
		},
		{
			name:    "different thread",
			line:    `[12:34:56] [main/INFO]: <Steve> Hello world`,
			wantNil: true,
		},
		{
			name:    "missing log prefix",
			line:    `<Steve> Hello world`,
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev := ParseLine(tt.line)

			if tt.wantNil {
				if ev != nil {
					t.Fatalf("ParseLine() = %+v, want nil", ev)
				}
				return
			}

			if ev == nil {
				t.Fatalf("ParseLine() = nil, want event")
			}
			if ev.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", ev.Type, tt.wantType)
			}
			if ev.Player != tt.wantPlayer {
				t.Errorf("Player = %q, want %q", ev.Player, tt.wantPlayer)
			}
			if ev.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", ev.Message, tt.wantMessage)
			}
			if ev.RawLine != tt.line {
				t.Errorf("RawLine = %q, want %q", ev.RawLine, tt.line)
			}
			if got := ev.Timestamp.Format("15:04:05"); got != "12:34:56" {
				t.Errorf("Timestamp = %q, want %q", got, "12:34:56")
			}
		})
	}
}

func TestTailerOnlyReportsNewLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "latest.log")
	if err := os.WriteFile(path, []byte("[10:00:00] [Server thread/INFO]: <Steve> old\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tailer := NewTailer(path, 5*time.Millisecond)
	events, err := tailer.Tail(ctx)
	if err != nil {
		t.Fatalf("Tail() error = %v", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	if _, err := f.WriteString("[10:00:01] [Server thread/INFO]: <Steve> new\n"); err != nil {
		t.Fatal(err)
	}

	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("event channel closed before an event was received")
		}
		if ev.Type != EventChat || ev.Player != "Steve" || ev.Message != "new" {
			t.Fatalf("got %+v, want chat from Steve with message %q", ev, "new")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for appended log line")
	}
}

func TestEventTypeString(t *testing.T) {
	tests := map[EventType]string{
		EventChat:        "chat",
		EventJoin:        "join",
		EventLeave:       "leave",
		EventDeath:       "death",
		EventAdvancement: "advancement",
		EventServerMsg:   "server",
		EventType(99):    "unknown",
	}

	for typ, want := range tests {
		if got := typ.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", typ, got, want)
		}
	}
}

func TestEventString(t *testing.T) {
	ev := Event{Type: EventChat, Player: "Steve", Message: "  hello  "}
	if got := ev.String(); got != "[chat] <Steve>   hello" {
		t.Fatalf("String() = %q, want %q", got, "[chat] <Steve>   hello")
	}
	if !strings.Contains(ev.String(), "Steve") {
		t.Fatal("String() does not contain player name")
	}
}
