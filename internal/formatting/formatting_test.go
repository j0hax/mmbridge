package formatting

import "testing"

func TestStripEscapes(t *testing.T) {
	tests := map[string]string{
		"§aHello §lWorld": "Hello World",
		"plain text":      "plain text",
		"§r":              "",
	}

	for in, want := range tests {
		if got := StripEscapes(in); got != want {
			t.Errorf("StripEscapes(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMinecraftToMarkdown(t *testing.T) {
	tests := map[string]string{
		"§lBold§r plain":     "**Bold** plain",
		"§oItalic§r":         "*Italic*",
		"§nUnderline§r":      "<u>Underline</u>",
		"§mStrike§r":         "~~Strike~~",
		"§lBold§aGreen text": "**Bold**Green text",
		"§kObfuscated§r":     "Obfuscated",
		"hëllo":              "hëllo",
	}

	for in, want := range tests {
		if got := MinecraftToMarkdown(in); got != want {
			t.Errorf("MinecraftToMarkdown(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMarkdownToMinecraft(t *testing.T) {
	tests := map[string]string{
		"**bold**":     "§lbold§r",
		"*italic*":     "§oitalic§r",
		"~~strike~~":   "§mstrike§r",
		"<u>under</u>": "§nunder§r",
		"plain text":   "plain text",
		"**unclosed":   "§lunclosed§r",
	}

	for in, want := range tests {
		if got := MarkdownToMinecraft(in); got != want {
			t.Errorf("MarkdownToMinecraft(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSimpleRoundTrip(t *testing.T) {
	tests := []string{
		"§lBold§r",
		"§oItalic§r",
		"§nUnderline§r",
		"§mStrike§r",
	}

	for _, in := range tests {
		md := MinecraftToMarkdown(in)
		if got := MarkdownToMinecraft(md); got != in {
			t.Errorf("round trip %q -> %q -> %q, want %q", in, md, got, in)
		}
	}
}
