package mcpserver

// Output schemas of the author and deploy tools: the JSON of the services'
// results (service.ValidateResult, DraftResult, DraftView, DeployResult,
// flow.DryRunResult, TestNodeResult) and of NodeTypeList, written like the
// read tools' (reads_schemas.go): every always-present field required, open
// objects, no formats or type unions; lists are never null
// (withEmptySlices). TestAuthorOutputSchemasMatchTheResults keeps them in
// step with the Go types.

const (
	issueSchema = `{"type":"object","required":["path","code","severity","message"],"properties":{` +
		`"path":{"type":"string","description":"Where: /name, /nodes/{nodeId}, /nodes/{nodeId}/config/{setting}, /connections/{id} or /ops/{index}."},` +
		`"code":{"type":"string","description":"A stable code, e.g. unknown_type, config_type, invalid_config, undefined_secret."},` +
		`"severity":{"type":"string","enum":["error","warning"]},` +
		`"message":` + stringSchema + `}}`
	issuesSchema = `{"type":"array","items":` + issueSchema + `}`

	alertSchema = `{"type":"object","required":["kind","message"],"properties":{` +
		`"kind":{"type":"string","description":"What the admin is warned about: risk_raised, privileged_node_added, destination_changed, ` +
		`dynamic_destination, new_host, secret_added, secret_moved, risk_switch_on, sensitive_literal, credentialed_request_changed."},` +
		`"nodeId":` + stringSchema + `,"path":` + stringSchema + `,"message":` + stringSchema + `,` +
		`"before":{"description":"The value before, when agents may read it."},` +
		`"after":{"description":"The value after, when agents may read it."}}}`
	alertsSchema = `{"type":"array","items":` + alertSchema + `,` +
		`"description":"What the admin's approval page flags, compared with the flow's current version."}`

	validateResultSchema = `{"type":"object","required":["valid","issues","risk"],"properties":{` +
		`"valid":{"type":"boolean","description":"No issue blocks saving a draft (an undefined_secret error still blocks a deploy)."},` +
		`"issues":` + issuesSchema + `,"risk":` + riskSchema + `}}`

	draftResultSchema = `{"type":"object","required":["draftId","status","risk","issues","alerts","flowId","baseVersion"],"properties":{` +
		`"draftId":` + stringSchema + `,"status":{"type":"string","description":"pending: the admin decides in PiWorker's editor."},` +
		`"risk":` + riskSchema + `,"issues":` + issuesSchema + `,"alerts":` + alertsSchema + `,` +
		`"flowId":{"type":"string","description":"The flow the draft changes (empty for a new flow)."},` +
		`"baseVersion":{"type":"integer","description":"The version the draft is based on (0 for a new flow)."}}}`

	draftFlowViewSchema = `{"type":"object","required":["name","nodes","connections","totalNodes","totalConnections","truncated"],` +
		`"description":"The proposed flow; settings agents may not read are \"<redacted>\".","properties":{` +
		`"id":` + stringSchema + `,"name":` + stringSchema + `,` +
		`"nodes":{"type":"array","items":` + nodeViewSchema + `},` +
		`"connections":{"type":"array","items":` + connectionViewSchema + `},` +
		`"totalNodes":` + integerSchema + `,"totalConnections":` + integerSchema + `,` +
		`"truncated":{"type":"boolean","description":"Nodes, connections or settings were left out to keep the view small."}}}`

	draftViewSchema = `{"type":"object","required":["draftId","status","risk","issues","alerts","flowId","baseVersion","note","createdAt","flow"],"properties":{` +
		`"draftId":` + stringSchema + `,` +
		`"status":{"type":"string","enum":["pending","approved","rejected","void"]},` +
		`"reason":{"type":"string","description":"The admin's reason for a rejection, or why the draft was voided."},` +
		`"risk":` + riskSchema + `,"issues":` + issuesSchema + `,"alerts":` + alertsSchema + `,` +
		`"flowId":` + stringSchema + `,"baseVersion":` + integerSchema + `,` +
		`"appliedVersion":{"type":"integer","description":"The version an approval saved."},` +
		`"note":` + stringSchema + `,"createdAt":` + timeSchema + `,"decidedAt":` + timeSchema + `,` +
		`"flow":` + draftFlowViewSchema + `}}`

	portPreviewSchema = `{"type":"object","required":["port","preview"],"properties":{` +
		`"port":` + stringSchema + `,"preview":{"type":"string","description":"The payload's JSON, cut to 2 KiB."}}}`
	portPreviewsSchema = `{"type":"array","items":` + portPreviewSchema + `}`

	traceStepSchema = `{"type":"object","required":["nodeId","type","outputs","durationMs","stubbed"],"properties":{` +
		`"nodeId":` + stringSchema + `,"type":` + stringSchema + `,` +
		`"input":{"type":"string","description":"The input payload's JSON, cut to 2 KiB."},` +
		`"outputs":` + portPreviewsSchema + `,"error":` + stringSchema + `,"durationMs":` + numberSchema + `,` +
		`"stubbed":{"type":"boolean","description":"The node did not run (a trigger, or a node that is not safe); it emitted its mock, if any."}}}`

	wouldExecuteSchema = `{"type":"object","required":["nodeId","type","risk","input"],"properties":{` +
		`"nodeId":` + stringSchema + `,"type":` + stringSchema + `,"risk":` + riskSchema + `,"input":` + stringSchema + `}}`

	dryRunResultSchema = `{"type":"object","required":["issues","trace","wouldExecute","missingSecrets","truncated","timedOut","capped"],"properties":{` +
		`"issues":` + issuesSchema + `,` +
		`"trace":{"type":"array","items":` + traceStepSchema + `,"description":"The node executions, in order (the trigger first)."},` +
		`"wouldExecute":{"type":"array","items":` + wouldExecuteSchema + `,"description":"Nodes a message reached that were not run."},` +
		`"missingSecrets":{"type":"array","items":` + stringSchema + `,"description":"Secret names referenced (the sandbox has none)."},` +
		`"truncated":` + booleanSchema + `,"timedOut":` + booleanSchema + `,"capped":` + booleanSchema + `}}`

	testNodeResultSchema = `{"type":"object","required":["issues","outputs","durationMs","truncated","timedOut"],"properties":{` +
		`"issues":` + issuesSchema + `,"outputs":` + portPreviewsSchema + `,"error":` + stringSchema + `,` +
		`"durationMs":` + numberSchema + `,"truncated":` + booleanSchema + `,"timedOut":` + booleanSchema + `}}`

	deployResultSchema = `{"type":"object","required":["flowId","version","running","risk"],"properties":{` +
		`"flowId":` + stringSchema + `,"version":{"type":"integer","description":"The version now running (or stopped)."},` +
		`"running":` + booleanSchema + `,"risk":` + riskSchema + `}}`

	portSchema = `{"type":"object","required":["id","name","dataType","multiple","required"],"properties":{` +
		`"id":` + stringSchema + `,"name":` + stringSchema + `,"dataType":` + stringSchema + `,` +
		`"multiple":` + booleanSchema + `,"required":` + booleanSchema + `}}`

	nodeTypeViewSchema = `{"type":"object","required":["type","name","category","risk","description","inputs","outputs","config"],"properties":{` +
		`"type":` + stringSchema + `,"name":` + stringSchema + `,` +
		`"category":{"type":"string","description":"input (triggers), processing or output (actions)."},` +
		`"risk":` + riskSchema + `,"description":` + stringSchema + `,` +
		`"inputs":{"type":"array","items":` + portSchema + `},"outputs":{"type":"array","items":` + portSchema + `},` +
		`"config":{"type":"object","required":["properties","required"],"properties":{` +
		`"properties":{"description":"The settings by name: type, title, description, default, enum, minimum, maximum, unit, ` +
		`and the markers sensitive, agentVisible, destination, riskSwitch, operation (present only when true)."},` +
		`"required":{"type":"array","items":` + stringSchema + `}}},` +
		`"documentation":{"type":"string","description":"Markdown."},` +
		`"documentationOmitted":{"type":"boolean","description":"Left out to keep the list small: ask for this type alone."}}}`

	nodeTypeListSchema = `{"type":"object","required":["types","total","truncated"],"properties":{` +
		`"types":{"type":"array","items":` + nodeTypeViewSchema + `},` +
		`"total":{"type":"integer","description":"The node types matching the filters."},` +
		`"truncated":{"type":"boolean","description":"Types or documentation were left out or cut to keep the list small."}}}`
)
