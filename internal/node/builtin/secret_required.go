package builtin

import "github.com/Pegasus8/piworker/internal/node"

// The settings below cannot work without the secrets they reference: deploy
// refuses a flow while one is not defined (node.SecretRequirer). They are
// resolved per execution, through the node's context.
var (
	_ node.SecretRequirer = (*TelegramAction)(nil)
	_ node.SecretRequirer = (*NotifyAction)(nil)
	_ node.SecretRequirer = (*DatabaseAction)(nil)
)

// RequiredSecrets: the bot token's secrets.
func (a *TelegramAction) RequiredSecrets() map[string][]string {
	return requiredSecrets("botToken", a.botToken)
}

// RequiredSecrets: the webhook URL's secrets.
func (a *NotifyAction) RequiredSecrets() map[string][]string {
	return requiredSecrets("url", a.url)
}

// RequiredSecrets: the DSN's secrets.
func (a *DatabaseAction) RequiredSecrets() map[string][]string {
	return requiredSecrets("dsn", a.dsn)
}

// requiredSecrets maps a property to the secret names its value references.
func requiredSecrets(prop, value string) map[string][]string {
	out := map[string][]string{}
	if names := secretNames(value); len(names) > 0 {
		out[prop] = names
	}
	return out
}

// secretNames returns the names of the {{secret.NAME}} placeholders of s, in
// order of appearance, each once.
func secretNames(s string) []string {
	var names []string
	seen := map[string]bool{}
	for _, match := range templateRe.FindAllString(s, -1) {
		name, ok := secretRef(placeholderParts(match))
		if ok && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}
