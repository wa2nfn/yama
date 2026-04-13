package parser

import "strings"

func CleanSpaces(s string) string {
	if s == "" {
		return ""
	}
	return strings.Join(strings.Fields(s), " ")
}

func StripNonLettersDigits(s string) string {
	if s == "" {
		return ""
	}

	var out strings.Builder
	out.Grow(len(s))

	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= 'a' && r <= 'z':
			out.WriteRune(r - 32)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == ' ':
			out.WriteRune(' ')
		case r == '/':
			out.WriteRune('/')
		}
	}

	return CleanSpaces(out.String())
}

func NormalizeWord(s string) string {
	if s == "" {
		return ""
	}

	s = CleanSpaces(s)

	var out strings.Builder
	out.Grow(len(s))

	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z':
			out.WriteRune(r)
		case r >= 'a' && r <= 'z':
			out.WriteRune(r - 32)
		case r >= '0' && r <= '9':
			out.WriteRune(r)
		}
	}

	return out.String()
}
