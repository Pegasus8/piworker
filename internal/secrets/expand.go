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
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(match, "{{"), "}}"))
		parts := strings.SplitN(inner, ".", 2)
		if parts[0] != "secret" {
			return match
		}
		if len(parts) < 2 {
			return ""
		}
		v, _ := store.Get(parts[1])
		return v
	})
}
