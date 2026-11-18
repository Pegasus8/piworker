package node

// SecretRequirer is implemented by nodes with settings that cannot work
// without the secrets they reference ({{secret.NAME}}): a bot token, a
// webhook URL, a database DSN. Deploy refuses a flow (a validation error,
// 400) while one of these secrets is not defined, instead of deploying it
// and failing every execution. The values are resolved when the node runs,
// through its context (secrets.FromContext), never at deploy; the dry-run
// does not check (its secret store is empty by design and it reports the
// referenced names instead).
type SecretRequirer interface {
	// RequiredSecrets returns, by config property, the names of the
	// secrets that property references, in order of appearance.
	RequiredSecrets() map[string][]string
}

// SecretReferencer is implemented by nodes with settings that resolve the
// secrets they reference ({{secret.NAME}}) when they run, but that a deploy
// does not refuse without them: optional credentials (an MQTT or SMTP
// username and password), where an undefined secret becomes an empty value,
// and an HTTP request's URL and headers, where it fails the request when it
// runs. Lint warns about each undefined one (flow.IssueUnresolvedSecret) and
// the deploy response carries the warning, so the admin learns at deploy
// time rather than from a failed or unauthenticated run. Settings whose
// secrets are required are SecretRequirer's, not listed here. Generic
// templates never resolve secrets and are not listed either.
type SecretReferencer interface {
	// SecretReferences returns, by config property, the names of the
	// secrets that property references, in order of appearance.
	SecretReferences() map[string][]string
}
