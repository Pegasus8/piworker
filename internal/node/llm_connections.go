package node

// LLMConnectionRequirer is implemented by nodes whose settings name an LLM
// connection (llm.Store, ADR-0038), such as process-llm. Deploy refuses a
// flow (a validation error, 400) while a named connection is not defined;
// Lint reports it as undefined_llm_connection, an environment issue the
// admin may still fix. The connection is resolved when the node runs,
// through its context (llm.FromContext), never at deploy.
type LLMConnectionRequirer interface {
	// RequiredLLMConnections returns, by config property, the LLM
	// connection name that property holds.
	RequiredLLMConnections() map[string]string
}
