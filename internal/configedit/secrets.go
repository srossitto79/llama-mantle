package configedit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"regexp"
	"sort"
	"strings"
)

// Hash identifies raw file contents.
func Hash(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

var (
	envRefRegex = regexp.MustCompile(`\$\{env\.([a-zA-Z_][a-zA-Z0-9_]*)\}`)
	// Placeholder for ${env.NAME}: the name is hex-encoded so it can never be
	// confused with surrounding text, and the token is a valid plain scalar.
	envPlaceholderRegex = regexp.MustCompile(`mantleenv0x([0-9a-f]+)x0`)
	// Matches by suffix only: "max_tokens" or "tokenizer" must not be redacted.
	secretNameRegex = regexp.MustCompile(`(?i)(api[_-]?keys?|token|secret|passw(or)?d|credentials?|auth|key)$`)
)

// isSecretName reports whether a key or env var name looks like it holds a
// secret (apiKey, HF_TOKEN, STRATA_API_KEY, DB_PASSWORD, ...).
func isSecretName(name string) bool {
	return secretNameRegex.MatchString(strings.TrimSpace(name))
}

// isReferenceOnly reports whether s holds only ${env.*} references, with an
// optional "Bearer" prefix, so showing it reveals nothing.
func isReferenceOnly(s string) bool {
	if !envRefRegex.MatchString(s) {
		return false
	}
	rest := strings.TrimSpace(envRefRegex.ReplaceAllString(s, ""))
	return rest == "" || strings.EqualFold(rest, "bearer")
}

// redactValue replaces literal secrets in v. key is the name v is stored
// under; it decides whether scalars are secret. Env-style "NAME=VALUE" strings
// are judged by NAME. Returns the redacted copy and whether anything changed.
func redactValue(key string, v any) (any, bool) {
	switch val := v.(type) {
	case string:
		if name, value, ok := strings.Cut(val, "="); ok && !strings.ContainsAny(name, " \t") && isSecretName(name) {
			if value == "" || isReferenceOnly(value) || value == Redacted {
				return val, false
			}
			return name + "=" + Redacted, true
		}
		if isSecretName(key) && val != "" && !isReferenceOnly(val) && val != Redacted {
			return Redacted, true
		}
		return val, false
	case []any:
		out := make([]any, len(val))
		changed := false
		for i, item := range val {
			r, c := redactValue(key, item)
			out[i] = r
			changed = changed || c
		}
		return out, changed
	case map[string]any:
		out := make(map[string]any, len(val))
		changed := false
		for k, item := range val {
			r, c := redactValue(k, item)
			out[k] = r
			changed = changed || c
		}
		return out, changed
	case nil, bool, int, int64, uint64, float64:
		if isSecretName(key) && val != nil {
			return Redacted, true
		}
		return val, false
	default:
		return val, false
	}
}

// containsRedacted reports whether v contains the Redacted marker anywhere.
func containsRedacted(v any) bool {
	switch val := v.(type) {
	case string:
		return strings.Contains(val, Redacted)
	case []any:
		for _, item := range val {
			if containsRedacted(item) {
				return true
			}
		}
	case map[string]any:
		for _, item := range val {
			if containsRedacted(item) {
				return true
			}
		}
	}
	return false
}

// withEnvPlaceholders swaps every ${env.NAME} for an opaque token, so the
// config can be loaded without ever reading environment values.
func withEnvPlaceholders(raw []byte) []byte {
	return envRefRegex.ReplaceAllFunc(raw, func(m []byte) []byte {
		name := envRefRegex.FindSubmatch(m)[1]
		return []byte("mantleenv0x" + hex.EncodeToString(name) + "x0")
	})
}

// restoreEnvRefs turns placeholders back into ${env.NAME} references.
func restoreEnvRefs(s string) string {
	return envPlaceholderRegex.ReplaceAllStringFunc(s, func(m string) string {
		name, err := hex.DecodeString(envPlaceholderRegex.FindStringSubmatch(m)[1])
		if err != nil {
			return m
		}
		return "${env." + string(name) + "}"
	})
}

// restoreEnvRefsDeep applies restoreEnvRefs to every string in v.
func restoreEnvRefsDeep(v any) any {
	switch val := v.(type) {
	case string:
		return restoreEnvRefs(val)
	case []any:
		for i := range val {
			val[i] = restoreEnvRefsDeep(val[i])
		}
	case map[string]any:
		for k := range val {
			val[k] = restoreEnvRefsDeep(val[k])
		}
	}
	return v
}

// referencedEnvNames lists the env var names raw refers to, sorted.
func referencedEnvNames(raw []byte) []string {
	seen := map[string]bool{}
	for _, m := range envRefRegex.FindAllSubmatch(raw, -1) {
		seen[string(m[1])] = true
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// scrubEnvValues replaces every value of an env var referenced by raw with
// its ${env.NAME} reference. Longest values first, so a value that contains
// another is replaced whole.
func scrubEnvValues(raw []byte, msg string) string {
	type pair struct{ name, value string }
	var pairs []pair
	for _, name := range referencedEnvNames(raw) {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			pairs = append(pairs, pair{name, v})
		}
	}
	sort.Slice(pairs, func(i, j int) bool { return len(pairs[i].value) > len(pairs[j].value) })
	for _, p := range pairs {
		msg = strings.ReplaceAll(msg, p.value, "${env."+p.name+"}")
	}
	return msg
}
