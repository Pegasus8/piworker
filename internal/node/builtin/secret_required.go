package builtin

import (
	"maps"
	"slices"

	"github.com/Pegasus8/piworker/internal/node"
)

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
	return secretsByProp(map[string]string{"botToken": a.botToken})
}

// RequiredSecrets: the webhook URL's secrets.
func (a *NotifyAction) RequiredSecrets() map[string][]string {
	return secretsByProp(map[string]string{"url": a.url})
}

// RequiredSecrets: the DSN's secrets.
func (a *DatabaseAction) RequiredSecrets() map[string][]string {
	return secretsByProp(map[string]string{"dsn": a.dsn})
}

// secretsByProp maps each property of values to the secret names its value
// references, leaving out the properties that reference none.
func secretsByProp(values map[string]string) map[string][]string {
	out := map[string][]string{}
	for prop, v := range values {
		if names := secretNames(v); len(names) > 0 {
			out[prop] = names
		}
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

// The settings below resolve the secrets they reference when the node runs
// but work, or fail only when they run, without them: Lint and deploy warn
// about an undefined one (node.SecretReferencer) instead of refusing.
var (
	_ node.SecretReferencer = (*EmailAction)(nil)
	_ node.SecretReferencer = (*MqttAction)(nil)
	_ node.SecretReferencer = (*MqttTrigger)(nil)
	_ node.SecretReferencer = (*HTTPAction)(nil)
)

// SecretReferences: the SMTP username's and password's secrets.
func (a *EmailAction) SecretReferences() map[string][]string {
	return secretsByProp(map[string]string{"username": a.username, "password": a.password})
}

// SecretReferences: the own broker username's and password's secrets (a
// named connection's credentials are the connection's).
func (a *MqttAction) SecretReferences() map[string][]string {
	return a.target.secretReferences()
}

// SecretReferences: the own broker username's and password's secrets.
func (t *MqttTrigger) SecretReferences() map[string][]string {
	return t.target.secretReferences()
}

// SecretReferences: the secrets of the URL and of the header values (in
// header name order), which fail the request when undefined.
func (a *HTTPAction) SecretReferences() map[string][]string {
	out := secretsByProp(map[string]string{"url": a.url})
	var names []string
	for _, k := range slices.Sorted(maps.Keys(a.headers)) {
		for _, name := range secretNames(a.headers[k]) {
			if !slices.Contains(names, name) {
				names = append(names, name)
			}
		}
	}
	if len(names) > 0 {
		out["headers"] = names
	}
	return out
}

// The MQTT nodes may name a broker connection (ADR-0029): deploy refuses an
// undefined one (node.ConnectionRequirer, Lint's undefined_connection).
var (
	_ node.ConnectionRequirer = (*MqttAction)(nil)
	_ node.ConnectionRequirer = (*MqttTrigger)(nil)
)

// RequiredConnections: the named connection, if any.
func (a *MqttAction) RequiredConnections() map[string]string { return a.target.requiredConnections() }

// RequiredConnections: the named connection, if any.
func (t *MqttTrigger) RequiredConnections() map[string]string { return t.target.requiredConnections() }
