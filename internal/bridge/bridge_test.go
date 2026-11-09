package bridge

import "testing"

func TestJSONEscape(t *testing.T) {
	tests := map[string]string{
		"plain text":         "plain text",
		`say "hello"`:        `say \"hello\"`,
		`C:\path\to\file`:    `C:\\path\\to\\file`,
		`quote " and \ slash`: `quote \" and \\ slash`,
		"ümlaut":             "ümlaut",
	}

	for in, want := range tests {
		if got := jsonEscape(in); got != want {
			t.Errorf("jsonEscape(%q) = %q, want %q", in, got, want)
		}
	}
}
