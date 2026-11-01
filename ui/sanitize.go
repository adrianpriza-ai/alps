package ui

import "strings"

// SanitizeRemote strips terminal control sequences from remote-derived text
// (AUR RPC fields, manifest names/descriptions) before it is printed.
// Escape sequences in untrusted output can rewrite the screen or window
// title, forge prompt lines, or overwrite the clipboard (OSC 52) on
// supporting terminals, so every control byte is removed while printable
// text, tabs and newlines pass through untouched.
func SanitizeRemote(s string) string {
	if !strings.ContainsFunc(s, isControlByte) {
		return s
	}
	return strings.Map(func(r rune) rune {
		if isControlByte(r) {
			return -1
		}
		return r
	}, s)
}

// isControlByte reports whether r is a C0 control (including ESC), DEL, or a
// C1 control in the 0x80–0x9f range. Tab and newline are preserved.
func isControlByte(r rune) bool {
	switch {
	case r == '\n' || r == '\t':
		return false
	case r < 0x20:
		return true
	case r == 0x7f:
		return true
	case r >= 0x80 && r <= 0x9f:
		return true
	}
	return false
}
