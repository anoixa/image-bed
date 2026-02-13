package utils

import (
	"strings"
	"unicode"
)

func SanitizeLogMessage(msg string) string {
	var sb strings.Builder
	for _, r := range msg {
		if r == 10 || r == 9 {
			sb.WriteRune(r)
		} else if unicode.IsPrint(r) || unicode.IsGraphic(r) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func SanitizeLogUsername(username string) string {
	if len(username) > 50 {
		username = username[:50] + "..."
	}
	return SanitizeLogMessage(username)
}
