package config

import "strings"

// BuildCommandString joins argv back into a shell command string, quoting
// any token that contains whitespace or a double quote. It is the inverse of
// SanitizeCommand (which tokenizes via shlex), letting callers round-trip a
// structured argv representation back into the freeform `cmd:` string
// without duplicating platform-specific shell-quoting rules.
func BuildCommandString(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = quoteArg(arg)
	}
	return strings.Join(parts, " ")
}

func quoteArg(arg string) string {
	if arg == "" {
		return `""`
	}
	if !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range arg {
		if r == '"' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteByte('"')
	return b.String()
}
