package matrix

import (
	"testing"

	"github.com/johannes/mmbridge/internal/config"
	"maunium.net/go/mautrix/id"
)

func TestParseListenAddress(t *testing.T) {
	tests := []struct {
		addr     string
		wantHost string
		wantPort uint16
		wantErr  bool
	}{
		{addr: ":8009", wantHost: "0.0.0.0", wantPort: 8009},
		{addr: "0.0.0.0:8009", wantHost: "0.0.0.0", wantPort: 8009},
		{addr: "127.0.0.1:1234", wantHost: "127.0.0.1", wantPort: 1234},
		{addr: "localhost:8009", wantHost: "localhost", wantPort: 8009},
		{addr: "8009", wantErr: true},
		{addr: ":not-a-port", wantErr: true},
		{addr: "localhost:not-a-port", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			host, port, err := parseListenAddress(tt.addr)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseListenAddress(%q) error = nil, want error", tt.addr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseListenAddress(%q) error = %v", tt.addr, err)
			}
			if host != tt.wantHost || port != tt.wantPort {
				t.Fatalf("parseListenAddress(%q) = (%q, %d), want (%q, %d)",
					tt.addr, host, port, tt.wantHost, tt.wantPort)
			}
		})
	}
}

func TestIsOwnUser(t *testing.T) {
	s := &Service{
		cfg: &config.MatrixConfig{
			Domain:      "example.com",
			BotUsername: "minecraft",
			UserPrefix:  "mc_",
		},
	}

	tests := []struct {
		userID id.UserID
		want   bool
	}{
		{userID: "@minecraft:example.com", want: true},
		{userID: "@mc_steve:example.com", want: true},
		{userID: "@mc_:example.com", want: true},
		{userID: "@alice:example.com", want: false},
		{userID: "@minecraft:other.example", want: false},
		{userID: "@mc_steve:other.example", want: false},
		{userID: "not-a-user-id", want: false},
	}

	for _, tt := range tests {
		t.Run(string(tt.userID), func(t *testing.T) {
			if got := s.isOwnUser(tt.userID); got != tt.want {
				t.Fatalf("isOwnUser(%q) = %v, want %v", tt.userID, got, tt.want)
			}
		})
	}
}
