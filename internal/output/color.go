package output

import "bytes"

const (
	colorReset  = "\x1b[0m"
	colorKey    = "\x1b[34;1m" // bold blue
	colorString = "\x1b[32m"   // green
	colorNumber = "\x1b[36m"   // cyan
	colorBool   = "\x1b[33m"   // yellow
	colorNull   = "\x1b[90m"   // gray
)

// Colorize adds ANSI colors to a valid JSON document (indented or compact).
func Colorize(doc []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(doc) * 2)
	for i := 0; i < len(doc); {
		c := doc[i]
		switch {
		case c == '"':
			end := scanString(doc, i)
			color := colorString
			if isKey(doc, end) {
				color = colorKey
			}
			out.WriteString(color)
			out.Write(doc[i:end])
			out.WriteString(colorReset)
			i = end
		case c == '-' || (c >= '0' && c <= '9'):
			end := i + 1
			for end < len(doc) && isNumberByte(doc[end]) {
				end++
			}
			out.WriteString(colorNumber)
			out.Write(doc[i:end])
			out.WriteString(colorReset)
			i = end
		case bytes.HasPrefix(doc[i:], []byte("true")):
			i = writeColored(&out, colorBool, "true", i)
		case bytes.HasPrefix(doc[i:], []byte("false")):
			i = writeColored(&out, colorBool, "false", i)
		case bytes.HasPrefix(doc[i:], []byte("null")):
			i = writeColored(&out, colorNull, "null", i)
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.Bytes()
}

func writeColored(out *bytes.Buffer, color, lit string, i int) int {
	out.WriteString(color)
	out.WriteString(lit)
	out.WriteString(colorReset)
	return i + len(lit)
}

// scanString returns the index just past the closing quote of the string starting at i.
func scanString(doc []byte, i int) int {
	for j := i + 1; j < len(doc); j++ {
		switch doc[j] {
		case '\\':
			j++
		case '"':
			return j + 1
		}
	}
	return len(doc)
}

// isKey reports whether the next non-space byte after a string is ':'.
func isKey(doc []byte, i int) bool {
	for ; i < len(doc); i++ {
		switch doc[i] {
		case ' ', '\t', '\n', '\r':
			continue
		case ':':
			return true
		default:
			return false
		}
	}
	return false
}

func isNumberByte(c byte) bool {
	return (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-'
}
