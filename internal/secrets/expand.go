package secrets

import (
	"context"
	"regexp"
	"strings"
)

// placeholderRe matches a {{...}} placeholder, as the node templates parse
// them.
var placeholderRe = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// Expand replaces each {{secret.NAME}} reference of s with the value of
// NAME in the store of ctx (FromContext), or "" when it is not defined (or
// the reference names none), and
// leaves every other placeholder unchanged. It is how credentials such as
// an MQTT username or password are resolved when used.
func Expand(ctx context.Context, s string) string {
	if !strings.Contains(s, "{{") {
		return s
	}
	store := FromContext(ctx)
	return placeholderRe.ReplaceAllStringFunc(s, func(match string) string {
		name, ok := referenceName(match)
		if !ok {
			return match
		}
		if name == "" {
			return ""
		}
		v, _ := store.Get(name)
		return v
	})
}

// referenceName parses one {{...}} placeholder: whether it is a secret
// reference and the name it names ("" for {{secret}}).
func referenceName(match string) (string, bool) {
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
	parts := strings.SplitN(inner, ".", 2)
	if parts[0] != "secret" {
		return "", false
	}
	if len(parts) < 2 {
		return "", true
	}
	return parts[1], true
}

// References returns the secret names s references, as Expand resolves
// them, in order of appearance, each once (a reference without a name is
// left out). It is how a credential setting is told to use a secret.
func References(s string) []string {
	var names []string
	seen := map[string]bool{}
	for _, match := range placeholderRe.FindAllString(s, -1) {
		if name, ok := referenceName(match); ok && name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}
