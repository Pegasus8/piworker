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
