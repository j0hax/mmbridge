// Package formatting provides functions to format
// Minecraft text to Markdown and vice-versa.
package formatting

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	mcBold          = 'l'
	mcItalic        = 'o'
	mcUnderline     = 'n'
	mcStrikethrough = 'm'
	mcObfuscated    = 'k'
	mcReset         = 'r'
)

type style struct {
	bold          bool
	italic        bool
	underline     bool
	strikethrough bool
}

// Generic way to filter Minecraft escapes
var MCEscapeCode = regexp.MustCompile(`§.`)

// MinecraftToMarkdown converts Minecraft legacy formatting codes
// such as §l, §o, §n, §m and §r to Markdown.
//
// Color codes are currently stripped and reset formatting.
// §k (obfuscated text) is stripped because Markdown has no equivalent.
func MinecraftToMarkdown(s string) string {
	var out strings.Builder
	var st style

	closeStyles := func() {
		// Close in reverse order.
		if st.strikethrough {
			out.WriteString("~~")
		}
		if st.underline {
			out.WriteString("</u>")
		}
		if st.italic {
			out.WriteString("*")
		}
		if st.bold {
			out.WriteString("**")
		}
	}

	openStyles := func() {
		if st.bold {
			out.WriteString("**")
		}
		if st.italic {
			out.WriteString("*")
		}
		if st.underline {
			out.WriteString("<u>")
		}
		if st.strikethrough {
			out.WriteString("~~")
		}
	}

	reset := func() {
		closeStyles()
		st = style{}
	}

	for i := 0; i < len(s); {
		if s[i] != '§' {
			r, size := utf8.DecodeRuneInString(s[i:])
			out.WriteRune(r)
			i += size
			continue
		}

		if i+2 > len(s) {
			out.WriteByte(s[i])
			i++
			continue
		}

		code := rune(s[i+2-1])

		switch code {
		case mcBold:
			if !st.bold {
				// Rebuild nesting cleanly.
				closeStyles()
				st.bold = true
				openStyles()
			}

		case mcItalic:
			if !st.italic {
				closeStyles()
				st.italic = true
				openStyles()
			}

		case mcUnderline:
			if !st.underline {
				closeStyles()
				st.underline = true
				openStyles()
			}

		case mcStrikethrough:
			if !st.strikethrough {
				closeStyles()
				st.strikethrough = true
				openStyles()
			}

		case mcReset:
			reset()

		case mcObfuscated:
			// No meaningful Markdown equivalent.

		default:
			// Minecraft color codes (0-9, a-f) reset formatting.
			if isColorCode(code) {
				reset()
			}
		}

		i += 2
	}

	closeStyles()

	return out.String()
}

func isColorCode(c rune) bool {
	return (c >= '0' && c <= '9') ||
		(c >= 'a' && c <= 'f') ||
		(c >= 'A' && c <= 'F')
}
