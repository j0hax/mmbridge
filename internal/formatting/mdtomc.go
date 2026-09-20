package formatting

import (
	"strings"
	"unicode/utf8"
)

// MarkdownToMinecraft converts a small Markdown subset to Minecraft
// legacy formatting codes.
//
// Supported:
//
//	**bold**
//	*italic*
//	~~strikethrough~~
//	<u>underline</u>
func MarkdownToMinecraft(s string) string {
	var out strings.Builder
	var st style

	restore := func() {
		if st.bold {
			out.WriteString("§l")
		}
		if st.italic {
			out.WriteString("§o")
		}
		if st.underline {
			out.WriteString("§n")
		}
		if st.strikethrough {
			out.WriteString("§m")
		}
	}

	resetAndRestore := func() {
		out.WriteString("§r")
		restore()
	}

	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "**"):
			if st.bold {
				st.bold = false
				resetAndRestore()
			} else {
				st.bold = true
				out.WriteString("§l")
			}
			i += 2

		case strings.HasPrefix(s[i:], "~~"):
			if st.strikethrough {
				st.strikethrough = false
				resetAndRestore()
			} else {
				st.strikethrough = true
				out.WriteString("§m")
			}
			i += 2

		case strings.HasPrefix(s[i:], "<u>"):
			if !st.underline {
				st.underline = true
				out.WriteString("§n")
			}
			i += len("<u>")

		case strings.HasPrefix(s[i:], "</u>"):
			if st.underline {
				st.underline = false
				resetAndRestore()
			}
			i += len("</u>")

		case s[i] == '*':
			if st.italic {
				st.italic = false
				resetAndRestore()
			} else {
				st.italic = true
				out.WriteString("§o")
			}
			i++

		default:
			r, size := utf8.DecodeRuneInString(s[i:])
			out.WriteRune(r)
			i += size
		}
	}

	if st != (style{}) {
		out.WriteString("§r")
	}

	return out.String()
}
