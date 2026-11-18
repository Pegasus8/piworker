package mcpserver

// Output schemas of the read tools: the JSON of the service's read views
// (service.FlowList, FlowView, RunList, RunView, SystemStatusView). Clients
// validate the structured content of successful results against them, so
// they list every field the views always carry as required and leave the
// objects open (a later version may add fields). Only keywords that every
// JSON Schema draft clients use reads the same way are used: no formats,
// no type unions. TestReadOutputSchemasMatchTheViews keeps them in step
// with the Go types.

const (
	stringSchema  = `{"type":"string"}`
	integerSchema = `{"type":"integer"}`
	numberSchema  = `{"type":"number"}`
	booleanSchema = `{"type":"boolean"}`
	timeSchema    = `{"type":"string","description":"RFC 3339 time (UTC)."}`

	flowStateSchema = `{"type":"string","enum":["running","stopped","failed"],` +
		`"description":"running; stopped; or failed: meant to run but not running."}`
	riskSchema = `{"type":"string","enum":["safe","external","privileged"]}`

	runPointerSchema = `{"type":"object","description":"The flow's latest run.",` +
		`"required":["id","status","startedAt"],"properties":{` +
		`"id":` + stringSchema + `,"status":{"type":"string","description":"running, success or error."},` +
		`"startedAt":` + timeSchema + `}}`

	// flowVersionSchema is a flow's saved version.
	flowVersionSchema = `{"type":"integer","description":"The flow's saved version: the baseVersion of a draft of it (pw_save_draft)."}`

	flowSummarySchema = `{"type":"object","required":["id","name","state","version","nodeCount","tools","toolCount"],"properties":{` +
		`"id":` + stringSchema + `,"name":` + stringSchema + `,"state":` + flowStateSchema + `,"version":` + flowVersionSchema + `,` +
		`"nodeCount":` + integerSchema + `,` +
		`"tools":{"type":"array","items":` + stringSchema + `,` +
		`"description":"Names of the tools the flow publishes while running (at most 20; toolCount counts them all)."},` +
		`"toolCount":` + integerSchema + `,"lastRun":` + runPointerSchema + `}}`

	flowListSchema = `{"type":"object","required":["flows","total","truncated"],"properties":{` +
		`"flows":{"type":"array","items":` + flowSummarySchema + `,"description":"The newest flows first."},` +
		`"total":{"type":"integer","description":"The number of stored flows."},` +
		`"truncated":{"type":"boolean","description":"Only the newest flows are listed."}}}`

	annotationsSchema = `{"type":"object","required":["readOnlyHint","destructiveHint","idempotentHint","openWorldHint"],"properties":{` +
		`"readOnlyHint":` + booleanSchema + `,"destructiveHint":` + booleanSchema + `,` +
		`"idempotentHint":` + booleanSchema + `,"openWorldHint":` + booleanSchema + `}}`

	nodeViewSchema = `{"type":"object","required":["id","type","name","enabled","position","config"],"properties":{` +
		`"id":` + stringSchema + `,"type":{"type":"string","description":"The node type, e.g. action-http."},` +
		`"name":{"type":"string","description":"The display name of the node's type."},` +
		`"enabled":` + booleanSchema + `,` +
		`"position":{"type":"object","required":["x","y"],"properties":{"x":` + numberSchema + `,"y":` + numberSchema + `}},` +
		`"risk":` + riskSchema + `,` +
		`"config":{"description":"The node's settings (an object): values not shown to agents read \"<redacted>\". ` +
		`null when configOmitted."},` +
		`"configOmitted":{"type":"boolean","description":"The settings were left out to keep the view small."}}}`

	connectionViewSchema = `{"type":"object","required":["id","sourceNode","sourcePort","targetNode","targetPort"],"properties":{` +
		`"id":` + stringSchema + `,"sourceNode":` + stringSchema + `,"sourcePort":` + stringSchema + `,` +
		`"targetNode":` + stringSchema + `,"targetPort":` + stringSchema + `}}`

	toolViewSchema = `{"type":"object","required":["name","triggerNodeId","risk","annotations","fingerprint","confirm","timeoutMs"],"properties":{` +
		`"name":` + stringSchema + `,"triggerNodeId":` + stringSchema + `,"title":` + stringSchema + `,` +
		`"risk":` + riskSchema + `,"annotations":` + annotationsSchema + `,` +
		`"fingerprint":{"type":"string","description":"Identifies the tool's contract; grants bind to it."},` +
		`"confirm":{"type":"string","description":"none, client or admin (the admin approves each call)."},` +
		`"timeoutMs":` + integerSchema + `}}`

	flowViewSchema = `{"type":"object","required":["id","name","state","version","nodes","connections","tools","totalNodes","totalConnections","truncated"],"properties":{` +
		`"id":` + stringSchema + `,"name":` + stringSchema + `,"state":` + flowStateSchema + `,"version":` + flowVersionSchema + `,` +
		`"nodes":{"type":"array","items":` + nodeViewSchema + `},` +
		`"connections":{"type":"array","items":` + connectionViewSchema + `},` +
		`"tools":{"type":"array","items":` + toolViewSchema + `,"description":"The tools the flow publishes while running."},` +
		`"totalNodes":` + integerSchema + `,"totalConnections":` + integerSchema + `,` +
		`"truncated":{"type":"boolean","description":"Nodes, connections, tools or settings were left out to keep the view small."}}}`

	runSummaryProperties = `"id":` + stringSchema + `,"flowId":` + stringSchema + `,` +
		`"status":{"type":"string","description":"running, success or error."},` +
		`"actor":{"type":"string","enum":["trigger","editor","agent","other"],"description":"Who started the run."},` +
		`"actorToken":{"type":"string","description":"The name of the agent's token."},` +
		`"startedAt":` + timeSchema + `,"finishedAt":` + timeSchema + `,"durationMs":` + integerSchema + `,` +
		`"nodeCount":{"type":"integer","description":"Node executions recorded."},` +
		`"errorCount":{"type":"integer","description":"Node executions that failed."}`
	runSummaryRequired = `"id","flowId","status","actor","startedAt","nodeCount","errorCount"`

	runListSchema = `{"type":"object","required":["runs"],"properties":{` +
		`"runs":{"type":"array","items":{"type":"object","required":[` + runSummaryRequired + `],"properties":{` + runSummaryProperties + `}},` +
		`"description":"Newest first."},` +
		`"nextBefore":{"type":"string","description":"Pass as before to list the next (older) page."}}}`

	runEventSchema = `{"type":"object","required":["nodeId","nodeType","phase","durationMs","at"],"properties":{` +
		`"nodeId":` + stringSchema + `,"nodeType":` + stringSchema + `,` +
		`"phase":{"type":"string","description":"success or error."},"durationMs":` + numberSchema + `,` +
		`"error":` + stringSchema + `,"at":` + timeSchema + `}}`

	runViewSchema = `{"type":"object","required":[` + runSummaryRequired + `,"events","totalEvents","truncated"],"properties":{` +
		runSummaryProperties + `,` +
		`"events":{"type":"array","items":` + runEventSchema + `,"description":"Node executions in order."},` +
		`"totalEvents":` + integerSchema + `,` +
		`"truncated":{"type":"boolean","description":"The last events were left out to keep the view small."}}}`

	systemStatusSchema = `{"type":"object","required":["version","platform","startedAt","uptimeSeconds","flows","tools","mcp","agentDeploy"],"properties":{` +
		`"version":` + stringSchema + `,"platform":{"type":"string","description":"OS/architecture, e.g. linux/arm64."},` +
		`"startedAt":` + timeSchema + `,"uptimeSeconds":` + integerSchema + `,` +
		`"flows":{"type":"object","required":["running","total"],"properties":{"running":` + integerSchema + `,"total":` + integerSchema + `}},` +
		`"tools":{"type":"integer","description":"Tools published by the running flows."},` +
		`"mcp":{"type":"object","required":["maxInflight","maxResultBytes"],"properties":{` +
		`"maxInflight":{"type":"integer","description":"Tool calls running at once, all tokens together."},` +
		`"maxResultBytes":{"type":"integer","description":"Flow tool replies larger than this are truncated (bytes); the built-in tools have bounds of their own."}}},` +
		`"agentDeploy":{"type":"string","enum":["off","low","all"],"description":"What agents may deploy."},` +
		`"disk":{"type":"object","required":["freeBytes","totalBytes"],"description":"The data directory's file system.",` +
		`"properties":{"freeBytes":` + integerSchema + `,"totalBytes":` + integerSchema + `}},` +
		`"memory":{"type":"object","required":["totalBytes","availableBytes"],"properties":{` +
		`"totalBytes":` + integerSchema + `,"availableBytes":` + integerSchema + `}},` +
		`"cpuTemperatureC":` + numberSchema + `}}`
)
