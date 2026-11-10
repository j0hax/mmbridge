package bridge

import (
	"testing"

	"github.com/johannes/mmbridge/internal/config"
)

func TestJSONEscape(t *testing.T) {
	tests := map[string]string{
		"plain text":          "plain text",
		`say "hello"`:         `say \"hello\"`,
		`C:\path\to\file`:     `C:\\path\\to\\file`,
		`quote " and \ slash`: `quote \" and \\ slash`,
		"ümlaut":              "ümlaut",
	}

	for in, want := range tests {
		if got := jsonEscape(in); got != want {
			t.Errorf("jsonEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReplaceGhostMentions(t *testing.T) {
	cfg := &config.Config{
		Matrix: config.MatrixConfig{
			UserPrefix: "mc_",
			Domain:     "example.com",
		},
	}

	b := &Bridge{
		cfg: cfg,
	}

	tests := map[string]string{
		// Basic mentions
		"Hello @mc_steve:example.com":                             "Hello steve",
		"@mc_steve:example.com joined":                            "steve joined",
		"Multiple @mc_steve:example.com and @mc_alex:example.com": "Multiple steve and alex",
		"No mention here":                                         "No mention here",
		"@mc_Player123:example.com":                               "Player123",
		"@mc_player_with_underscore:example.com":                  "player_with_underscore",

		// Case-insensitive matching
		"@MC_Steve:example.com": "Steve",
		"@Mc_Steve:example.com": "Steve",
		"@mc_STEVE:example.com": "STEVE",

		// Word boundary - should NOT match partial strings
		"@mc_steve:example.comextra": "@mc_steve:example.comextra",
		"@mc_steve:example.com.":     "steve.",
		"@mc_steve:example.com!":     "steve!",
		"@mc_steve:example.com ":     "steve ",

		// Wrong prefix or domain - should NOT be replaced
		"@wrong_prefix_steve:example.com": "@wrong_prefix_steve:example.com",
		"@mc_steve:wrongdomain.com":       "@mc_steve:wrongdomain.com",

		// Position variations
		"Before @mc_steve:example.com after": "Before steve after",
		"@mc_steve:example.com":              "steve",
		"End @mc_steve:example.com":          "End steve",
	}

	for in, want := range tests {
		if got := b.replaceGhostMentions(in); got != want {
			t.Errorf("replaceGhostMentions(%q) = %q, want %q", in, got, want)
		}
	}
}
