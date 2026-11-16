package service

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// The flow assistant (docs/design/llm.md §7, ADR-0041): the editor turns a
// description into a proposed flow. The assistant asks one of the admin's
// LLM connections for the flow as JSON, given a catalog of the registered
// node types and the names of the admin's connections, lays it out, checks
// it with flow.Lint and sends the blocking issues back for at most
// AssistantRepairRounds repairs. It stores, deploys and runs nothing: the
// admin previews the proposal and applies it to the canvas.

// Assistant bounds.
const (
	// AssistantTimeout bounds one request, repairs included.
	AssistantTimeout = 3 * time.Minute
	// AssistantMaxTokens caps each answer (a flow of a few dozen nodes).
	AssistantMaxTokens = 16000
	// AssistantRepairRounds bounds the requests that send blocking issues
	// back to the model.
	AssistantRepairRounds = 2
	// MaxAssistantDescriptionChars bounds the admin's description.
	MaxAssistantDescriptionChars = 4000
	// maxAssistantNotesChars and maxAssistantFlowDescriptionChars bound what
	// the answer may carry besides the flow (longer texts are cut).
	maxAssistantNotesChars           = 4000
	maxAssistantFlowDescriptionChars = 1000
)

// ErrAssistantModel: the model request failed (the provider's error, the
// connection's spent budget, the time bound) or its answer is not a flow.
// The error says why and may be shown to the admin: it never holds an API
// key (llm errors are redacted) nor a secret value.
var ErrAssistantModel = errors.New("the flow assistant's model request failed")

// AssistantOptions configures the flow assistant.
type AssistantOptions struct {
	// Registry lists the node types the model may use and checks the
	// proposal (default: an empty registry).
	Registry *node.Registry
	// LLM holds the LLM connections: the one a request names (whose budget
	// it spends) and the names the prompt lists; Lint checks the names
	// nodes use against it (default: an empty store).
	LLM *llm.Store
	// Brokers holds the MQTT broker connections whose names the prompt
	// lists and Lint checks (nil: none).
	Brokers *brokers.Store
	// Secrets resolve the connection's API key and are what Lint checks
	// {{secret.NAME}} references against (nil: none).
	Secrets *secrets.Store
	Logger  zerolog.Logger
}

// Assistant proposes flows from a description (ProposeFlow). It runs one
// request at a time.
type Assistant struct {
	registry *node.Registry
	conns    *llm.Store
	brokers  *brokers.Store
	secrets  *secrets.Store
	logger   zerolog.Logger
	timeout  time.Duration // AssistantTimeout (tests shorten it)

	// busy is held while a request runs: one at a time, the others are
	// refused, not queued (a request takes minutes and spends tokens).
	busy sync.Mutex

	catalogOnce sync.Once
	catalog     string
}

// NewAssistant creates the flow assistant.
func NewAssistant(opts AssistantOptions) *Assistant {
	if opts.Registry == nil {
		opts.Registry = node.NewRegistry()
	}
	if opts.LLM == nil {
		opts.LLM = llm.NewStore()
	}
	return &Assistant{
		registry: opts.Registry, conns: opts.LLM, brokers: opts.Brokers, secrets: opts.Secrets,
		logger: opts.Logger, timeout: AssistantTimeout,
	}
}

// AssistantFlowRequest is POST /api/llm/assistant/flow's body.
type AssistantFlowRequest struct {
	// Connection names the LLM connection to ask.
	Connection string `json:"connection"`
	// Model overrides the connection's default model.
	Model string `json:"model,omitempty"`
	// Description says what the flow should do (or how to change Flow).
	Description string `json:"description"`
	// Flow is the editor's current, unsaved definition, for "change this
	// flow" (nil: a new flow).
	Flow *AssistantFlow `json:"flow,omitempty"`
}

// AssistantFlow is a flow definition as the assistant reads and proposes
// it: the editor's nodes and connections, without ID, state or version.
type AssistantFlow struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Nodes       []flow.Node       `json:"nodes"`
	Connections []flow.Connection `json:"connections"`
}

// AssistantFlowResult is the assistant's proposal: the flow (every node
// with its registered category and ports, laid out; every connection with
// an ID), the model's notes for the admin, what flow.Lint still finds in it
// (never nil) and the tokens the requests spent.
type AssistantFlowResult struct {
	Flow   AssistantFlow `json:"flow"`
	Notes  string        `json:"notes"`
	Issues []flow.Issue  `json:"issues"`
	Usage  llm.Usage     `json:"usage"`
}

// ProposeFlow asks the request's LLM connection for a flow doing what the
// description says. Request errors are *ToolError (ErrInvalidArguments;
// ErrBusy while another request runs); a failed model request, or an answer
// that never is a flow, wraps ErrAssistantModel and the llm error
// (llm.ErrBudgetExhausted, llm.ErrInvalidAnswer, context.DeadlineExceeded,
// an *llm.APIError, ...). The answer's tokens are spent from the
// connection's daily budget, also when they fail.
func (a *Assistant) ProposeFlow(ctx context.Context, req AssistantFlowRequest) (AssistantFlowResult, error) {
	conn, model, err := a.checkRequest(&req)
	if err != nil {
		return AssistantFlowResult{}, err
	}
	if !a.busy.TryLock() {
		return AssistantFlowResult{}, toolError(ErrBusy, "the flow assistant is busy with another request; try again when it finishes")
	}
	defer a.busy.Unlock()

	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()
	// The stores the client spends from and Lint checks names against: the
	// assistant's, never the process-wide defaults (nil: empty ones).
	ctx = secrets.WithStore(brokers.WithStore(llm.WithStore(ctx, a.conns), a.brokers), a.secrets)
	client, err := llm.NewClient(ctx, conn)
	if err != nil {
		// The connection's own settings (an undefined secret in its key):
		// the message names the secret, never a value.
		return AssistantFlowResult{}, toolError(ErrInvalidArguments, err.Error())
	}

	current, withheld := currentView(req.Flow, a.registry)
	system := a.systemPrompt()
	messages := []llm.Message{{Role: llm.RoleUser, Text: userPrompt(strings.TrimSpace(req.Description), current)}}
	var usage llm.Usage
	for round := 0; ; round++ {
		resp, err := client.Chat(ctx, llm.Request{
			Model: model, System: system, Messages: messages, MaxTokens: AssistantMaxTokens, JSON: true, Cache: true,
		})
		if err != nil {
			return AssistantFlowResult{}, fmt.Errorf("%w: %w", ErrAssistantModel, err)
		}
		usage = usage.Add(resp.Usage)

		var feedback string
		answer, problem := decodeAnswer(resp.JSON)
		if problem != "" {
			if round == AssistantRepairRounds {
				return AssistantFlowResult{}, fmt.Errorf("%w: %w: %s", ErrAssistantModel, llm.ErrInvalidAnswer, problem)
			}
			feedback = "Your answer is not a flow: " + problem + ". Answer the whole flow as one JSON object " +
				`{"name", "description", "notes", "nodes", "connections"}, as described.`
		} else {
			f := a.buildFlow(answer, req.Flow, withheld)
			issues := flow.LintContext(ctx, f, a.registry)
			blocking := flow.BlockingIssues(issues)
			if len(blocking) == 0 || round == AssistantRepairRounds {
				a.logger.Info().Str("connection", conn.Name).Str("model", model).Int("requests", round+1).
					Int("nodes", len(f.Nodes)).Int("issues", len(issues)).Int("blocking", len(blocking)).
					Int64("inputTokens", usage.InputTokens).Int64("outputTokens", usage.OutputTokens).
					Msg("Flow assistant proposed a flow")
				return AssistantFlowResult{
					Flow:  AssistantFlow{Name: f.Name, Description: f.Description, Nodes: f.Nodes, Connections: f.Connections},
					Notes: answer.notes, Issues: issues, Usage: usage,
				}, nil
			}
			feedback = repairPrompt(f, blocking)
		}
		messages = append(messages, resp.Message, llm.Message{Role: llm.RoleUser, Text: feedback})
	}
}

// checkRequest validates req and returns the connection it names and the
// model to ask (the request's, else the connection's default).
func (a *Assistant) checkRequest(req *AssistantFlowRequest) (llm.Connection, string, error) {
	invalid := func(format string, args ...any) error {
		return toolError(ErrInvalidArguments, fmt.Sprintf(format, args...))
	}
	description := strings.TrimSpace(req.Description)
	switch {
	case description == "":
		return llm.Connection{}, "", invalid("describe the flow: the description is empty")
	case !utf8.ValidString(description):
		return llm.Connection{}, "", invalid("the description is not valid UTF-8")
	case utf8.RuneCountInString(description) > MaxAssistantDescriptionChars:
		return llm.Connection{}, "", invalid("the description is over %d characters", MaxAssistantDescriptionChars)
	}
	if f := req.Flow; f != nil {
		if len(f.Nodes) > MaxDraftNodes {
			return llm.Connection{}, "", invalid("the current flow has %d nodes; at most %d", len(f.Nodes), MaxDraftNodes)
		}
		if len(f.Connections) > MaxDraftConnections {
			return llm.Connection{}, "", invalid("the current flow has %d connections; at most %d", len(f.Connections), MaxDraftConnections)
		}
	}
	name := strings.TrimSpace(req.Connection)
	if name == "" {
		return llm.Connection{}, "", invalid("choose an LLM connection")
	}
	conn, ok := a.conns.Get(name)
	if !ok {
		return llm.Connection{}, "", invalid("LLM connection %q is not defined", name)
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = conn.Model
	}
	switch {
	case model == "":
		return llm.Connection{}, "", invalid("no model: choose one, or set a default model on LLM connection %q", name)
	case utf8.RuneCountInString(model) > llm.MaxModelLength || strings.ContainsAny(model, "\x00\r\n"):
		return llm.Connection{}, "", invalid("invalid model name (at most %d characters, one line)", llm.MaxModelLength)
	}
	return conn, model, nil
}

// --- The prompt ---

// systemPrompt is the flow model, the rules, the names of the admin's
// connections (never their settings) and the node catalog.
func (a *Assistant) systemPrompt() string {
	llmNames := a.conns.Names()
	var brokerNames []string
	if a.brokers != nil {
		brokerNames = a.brokers.Names()
	}
	var b strings.Builder
	b.WriteString(assistantRules)
	b.WriteString("\nThe admin's LLM connections (settings with options \"llm-connections\"): ")
	b.WriteString(namesList(llmNames))
	b.WriteString("\nThe admin's MQTT broker connections (settings with options \"connections\"): ")
	b.WriteString(namesList(brokerNames))
	b.WriteString("\n\nNode types, one JSON object per line (inputs and outputs are port IDs):\n")
	b.WriteString(a.nodeCatalog())
	return b.String()
}

func namesList(names []string) string {
	if len(names) == 0 {
		return "none defined yet"
	}
	return compactJSON(names)
}

const assistantRules = `You design flows for PiWorker, a visual automation system: a flow is a graph of nodes that pass messages along connections. The admin describes what a flow should do and you answer the flow as one JSON object. Nothing is saved, deployed or run until the admin reviews your proposal in the editor.

The flow model:
- nodes: [{"id", "type", "config"}]. id: a short unique name (letters, digits, '-' or '_'). type: one of the node types listed below, no other. config: the node's settings by name. Leave "position" out (PiWorker lays the nodes out) and "enabled" out (new nodes are enabled; a node you keep keeps its current state).
- connections: [{"sourceNode", "sourcePort", "targetNode", "targetPort"}]: a message leaves the node sourceNode through its output port sourcePort and enters the node targetNode through its input port targetPort.
- A flow starts at a trigger (category input; it has no input port): a schedule, a webhook, an incoming message. Processing nodes transform, filter or route messages; output nodes act (send, write, notify). No cycles, and no connection from a node to itself.
- The ports of a node are those its type lists. A type with "outputsFrom" also has one output port per item of that list setting (the port ID is the item). Every processing and output node also has the output port "error": when the node fails, a message {message, nodeId, nodeType, input} leaves through it; connect it where a failure needs handling.
- A message has a payload (any JSON value), a topic and meta. Text settings whose description says they are templates take placeholders: {{payload}} (the whole payload), {{payload.field.sub}}, {{topic}}, {{meta.key}}, {{id}}, {{timestamp}}, {{vars.NAME}} (a global variable), {{flowVars.NAME}} (a variable of the flow).

Settings:
- Use the settings a type lists, with their type, enum values and bounds. Set the required ones; leave out those whose default fits.
- Never write a credential (a password, token, API key, or a URL or DSN holding one) into a setting: write a {{secret.NAME}} reference to a secret the admin defines, with a clear NAME in capitals, and say in the notes which secrets to define. Settings marked sensitive take only such references.
- Settings with options "llm-connections" or "connections" take the name of one of the admin's connections listed below, exactly. If none fits, choose a clear name and say in the notes that the admin must create that connection. Leave settings with options "llm-models" empty to use the connection's default model.

Answer with exactly one JSON object and nothing else:
{"name": "a short name for the flow", "description": "one sentence", "notes": "what the admin must do or check before deploying it: secrets and connections to define, values to adjust", "nodes": [...], "connections": [...]}

When the admin gives the current flow, answer the whole flow after the change: keep the id, type and config of every node you keep (a kept node with "enabled": false stays disabled unless the admin asks to enable it). A "<redacted>" value is a credential PiWorker withheld from you: keep it as "<redacted>" and its value is kept.
`

// catalogType is a node type as the prompt lists it: what writing a node
// of it needs, without its documentation (long, and the settings say
// enough).
type catalogType struct {
	Type        string                    `json:"type"`
	Name        string                    `json:"name"`
	Category    string                    `json:"category"`
	Risk        string                    `json:"risk"`
	Description string                    `json:"description,omitempty"`
	Inputs      []string                  `json:"inputs,omitempty"`
	Outputs     []string                  `json:"outputs,omitempty"`
	OutputsFrom string                    `json:"outputsFrom,omitempty"`
	Settings    map[string]catalogSetting `json:"settings,omitempty"`
}

type catalogSetting struct {
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Required    bool     `json:"required,omitempty"`
	Default     any      `json:"default,omitempty"`
	Enum        []string `json:"enum,omitempty"`
	Minimum     *float64 `json:"minimum,omitempty"`
	Maximum     *float64 `json:"maximum,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Sensitive   bool     `json:"sensitive,omitempty"`
	Options     string   `json:"options,omitempty"`
}

// nodeCatalog lists the registered types, triggers first, then processing
// and output nodes, each by type ID. The registry does not change once the
// process runs, so it is built once.
func (a *Assistant) nodeCatalog() string {
	a.catalogOnce.Do(func() {
		infos := a.registry.List()
		slices.SortFunc(infos, func(x, y node.NodeTypeInfo) int {
			return cmp.Or(cmp.Compare(categoryRank(x.Category), categoryRank(y.Category)), cmp.Compare(x.Type, y.Type))
		})
		lines := make([]string, 0, len(infos))
		for _, info := range infos {
			lines = append(lines, compactJSON(catalogEntry(info)))
		}
		a.catalog = strings.Join(lines, "\n")
	})
	return a.catalog
}

func categoryRank(c types.NodeCategory) int {
	switch c {
	case types.NodeCategoryInput:
		return 0
	case types.NodeCategoryProcessing:
		return 1
	case types.NodeCategoryOutput:
		return 2
	}
	return 3
}

func catalogEntry(info node.NodeTypeInfo) catalogType {
	risk := info.Risk
	if !risk.Valid() {
		risk = node.RiskPrivileged // what flow.Risk assumes of what it cannot rate
	}
	t := catalogType{
		Type: info.Type, Name: info.Name, Category: string(info.Category), Risk: string(risk),
		Description: info.Description, OutputsFrom: info.OutputsFrom,
	}
	for _, p := range info.Inputs {
		t.Inputs = append(t.Inputs, p.ID)
	}
	for _, p := range info.Outputs {
		t.Outputs = append(t.Outputs, p.ID)
	}
	if info.Config != nil && len(info.Config.Properties) > 0 {
		t.Settings = make(map[string]catalogSetting, len(info.Config.Properties))
		for name, p := range info.Config.Properties {
			description := p.Description
			if description == "" {
				description = p.Title
			}
			t.Settings[name] = catalogSetting{
				Type: p.Type, Description: description, Required: slices.Contains(info.Config.Required, name),
				Default: p.Default, Enum: p.Enum, Minimum: p.Minimum, Maximum: p.Maximum, Unit: p.Unit,
				Sensitive: p.Sensitive, Options: p.Options,
			}
		}
	}
	return t
}

// viewNode is a node of the current flow as the prompt shows it.
type viewNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Position flow.Position  `json:"position"`
	Config   map[string]any `json:"config,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty"` // only when false
}

type viewConnection struct {
	SourceNode string `json:"sourceNode"`
	SourcePort string `json:"sourcePort"`
	TargetNode string `json:"targetNode"`
	TargetPort string `json:"targetPort"`
}

type viewFlow struct {
	Name        string           `json:"name,omitempty"`
	Description string           `json:"description,omitempty"`
	Nodes       []viewNode       `json:"nodes"`
	Connections []viewConnection `json:"connections"`
}

// withheldNode is what currentView withheld from a node: its type and the
// values of the settings it showed as RedactedValue.
type withheldNode struct {
	typ    string
	values map[string]any
}

// currentView renders the current flow for the prompt (nil: none) and
// returns what it withheld, by node ID. A credential never reaches the
// model: the value of a Sensitive setting that is not only {{secret.NAME}}
// references is shown as RedactedValue, and so is every value whose
// sensitivity is unknown (a setting the type does not declare, a node of an
// unregistered type). The request's flow is not modified.
func currentView(f *AssistantFlow, reg *node.Registry) (*viewFlow, map[string]withheldNode) {
	if f == nil {
		return nil, nil
	}
	view := &viewFlow{Name: f.Name, Description: f.Description, Nodes: []viewNode{}, Connections: []viewConnection{}}
	withheld := map[string]withheldNode{}
	for _, n := range f.Nodes {
		vn := viewNode{ID: n.ID, Type: n.Type, Position: n.Position}
		if !n.Enabled {
			vn.Enabled = new(bool)
		}
		var schema *node.ConfigSchema
		if info, ok := reg.Get(n.Type); ok {
			schema = info.Config
		}
		if len(n.Config) > 0 {
			vn.Config = make(map[string]any, len(n.Config))
		}
		for prop, v := range n.Config {
			known, sensitive := false, false
			if schema != nil {
				var p node.ConfigProperty
				p, known = schema.Properties[prop]
				sensitive = p.Sensitive
			}
			if (sensitive || !known) && !secretOnly(v) {
				vn.Config[prop] = RedactedValue
				withhold(withheld, n, prop, v)
				continue
			}
			vn.Config[prop] = v
		}
		view.Nodes = append(view.Nodes, vn)
	}
	for _, c := range f.Connections {
		view.Connections = append(view.Connections, viewConnection{
			SourceNode: c.SourceNode, SourcePort: c.SourcePort, TargetNode: c.TargetNode, TargetPort: c.TargetPort,
		})
	}
	return view, withheld
}

// withhold records v, the value of n's setting prop that the prompt
// withheld. Of nodes sharing an ID the first counts: one of another type
// under that ID gets nothing back.
func withhold(withheld map[string]withheldNode, n flow.Node, prop string, v any) {
	w, ok := withheld[n.ID]
	if !ok {
		w = withheldNode{typ: n.Type, values: map[string]any{}}
		withheld[n.ID] = w
	}
	if _, seen := w.values[prop]; w.typ == n.Type && !seen {
		w.values[prop] = v
	}
}

// userPrompt is the admin's description, with the current flow when there
// is one.
func userPrompt(description string, current *viewFlow) string {
	if current == nil {
		return "Build a flow that does this:\n" + description
	}
	return "Change the current flow as follows:\n" + description + "\n\nThe current flow:\n" + compactJSON(current)
}

// repairPrompt lists the blocking issues of f for the next request.
// Connections are named by their endpoints, in the path and the message:
// the model gave them no ID (buildFlow did).
func repairPrompt(f *flow.Flow, issues []flow.Issue) string {
	var b strings.Builder
	b.WriteString("PiWorker checked this flow and found problems that stop it from deploying:\n")
	for _, is := range issues {
		where, message := is.Path, is.Message
		if rest, ok := strings.CutPrefix(is.Path, "/connections/"); ok {
			for _, c := range f.Connections {
				if c.ID == rest {
					endpoints := fmt.Sprintf("%s.%s -> %s.%s", c.SourceNode, c.SourcePort, c.TargetNode, c.TargetPort)
					where, message = "connections", strings.ReplaceAll(message, c.ID, "("+endpoints+")")
					break
				}
			}
		}
		fmt.Fprintf(&b, "- %s: %s [%s]\n", where, message, is.Code)
	}
	b.WriteString("Fix them and answer the whole corrected flow as one JSON object, as before.")
	return b.String()
}

// compactJSON encodes v without HTML escaping ("<redacted>" stays
// readable); v is always encodable here.
func compactJSON(v any) string {
	b, err := marshalNoEscape(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// --- The answer ---

// assistantAnswer is a decoded answer of the model.
type assistantAnswer struct {
	name, description, notes string
	nodes                    []answerNode
	connections              []flow.Connection
}

type answerNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Position *flow.Position `json:"position"`
	Config   map[string]any `json:"config"`
	Enabled  *bool          `json:"enabled"`
}

type answerConnection struct {
	ID         string `json:"id"`
	SourceNode string `json:"sourceNode"`
	SourcePort string `json:"sourcePort"`
	TargetNode string `json:"targetNode"`
	TargetPort string `json:"targetPort"`
}

// decodeAnswer reads the model's JSON answer, or says (to the model) why
// it is not a flow.
func decodeAnswer(v any) (assistantAnswer, string) {
	obj, ok := v.(map[string]any)
	if !ok {
		return assistantAnswer{}, "the answer must be one JSON object, not an array or a value"
	}
	nodes, ok := obj["nodes"].([]any)
	if !ok {
		return assistantAnswer{}, `"nodes" must be an array of nodes`
	}
	if len(nodes) > MaxDraftNodes {
		return assistantAnswer{}, fmt.Sprintf("the flow has %d nodes; at most %d", len(nodes), MaxDraftNodes)
	}
	var conns []any
	if raw, present := obj["connections"]; present && raw != nil {
		if conns, ok = raw.([]any); !ok {
			return assistantAnswer{}, `"connections" must be an array of connections`
		}
	}
	if len(conns) > MaxDraftConnections {
		return assistantAnswer{}, fmt.Sprintf("the flow has %d connections; at most %d", len(conns), MaxDraftConnections)
	}
	p := assistantAnswer{
		name:        truncateRunes(textField(obj["name"]), MaxDraftNameChars),
		description: truncateRunes(textField(obj["description"]), maxAssistantFlowDescriptionChars),
		notes:       truncateRunes(textField(obj["notes"]), maxAssistantNotesChars),
	}
	for i, raw := range nodes {
		var n answerNode
		if !reencode(raw, &n) {
			return assistantAnswer{}, fmt.Sprintf(`nodes[%d] must be an object {"id": string, "type": string, "config": object}`, i)
		}
		p.nodes = append(p.nodes, n)
	}
	for i, raw := range conns {
		var c answerConnection
		if !reencode(raw, &c) {
			return assistantAnswer{}, fmt.Sprintf(`connections[%d] must be an object {"sourceNode", "sourcePort", "targetNode", "targetPort"} of strings`, i)
		}
		p.connections = append(p.connections, flow.Connection(c))
	}
	return p, ""
}

// reencode decodes the JSON value v (as ParseJSONAnswer decoded it) into
// dst, an object.
func reencode(v any, dst any) bool {
	if _, ok := v.(map[string]any); !ok {
		return false
	}
	data, err := json.Marshal(v)
	return err == nil && json.Unmarshal(data, dst) == nil
}

// textField is a string field of the answer; a list of strings is joined
// by lines (models write notes as lists).
func textField(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []any:
		var lines []string
		for _, e := range x {
			if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
				lines = append(lines, strings.TrimSpace(s))
			}
		}
		return strings.Join(lines, "\n")
	}
	return ""
}

func truncateRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// buildFlow turns a decoded answer into a flow: each node gets its
// registered category and ports (the outputs its config gives it), an
// empty config when it has none; each connection an ID. Values the prompt
// withheld come back on the nodes the model kept (same ID and type) where
// it wrote RedactedValue. A kept node without "enabled" keeps its current
// state (a node the admin disabled is never turned back on by a proposal),
// other nodes are enabled unless the answer says otherwise; a kept node
// without a position keeps its current one, the others are laid out.
func (a *Assistant) buildFlow(p assistantAnswer, current *AssistantFlow, withheld map[string]withheldNode) *flow.Flow {
	kept := map[string]flow.Node{} // of nodes sharing an ID, the first
	if current != nil {
		for _, n := range current.Nodes {
			if _, dup := kept[n.ID]; !dup {
				kept[n.ID] = n
			}
		}
	}
	f := &flow.Flow{Name: p.name, Description: p.description, Nodes: []flow.Node{}, Connections: []flow.Connection{}}
	var unplaced []string
	for _, pn := range p.nodes {
		n := flow.Node{ID: pn.ID, Type: pn.Type, Config: map[string]any{}, Enabled: true,
			Inputs: []types.Port{}, Outputs: []types.Port{}}
		for k, v := range pn.Config {
			if v != nil {
				n.Config[k] = v
			}
		}
		if w, ok := withheld[n.ID]; ok && w.typ == n.Type {
			for prop, v := range w.values {
				if s, ok := n.Config[prop].(string); ok && s == RedactedValue {
					n.Config[prop] = cloneJSONValue(v)
				}
			}
		}
		switch cur, ok := kept[n.ID]; {
		case pn.Enabled != nil:
			n.Enabled = *pn.Enabled
		case ok && cur.Type == n.Type:
			n.Enabled = cur.Enabled
		}
		if info, ok := a.registry.Get(n.Type); ok {
			n.Category = info.Category
			n.Inputs = append(n.Inputs, info.Inputs...)
			n.Outputs = node.ConfigOutputs(info, n.Config)
		}
		switch {
		case pn.Position != nil && *pn.Position != (flow.Position{}) && flow.ValidPosition(*pn.Position):
			n.Position = *pn.Position
		default:
			if cur, ok := kept[n.ID]; ok && cur.Position != (flow.Position{}) {
				n.Position = cur.Position
			} else {
				unplaced = append(unplaced, n.ID)
			}
		}
		f.Nodes = append(f.Nodes, n)
	}
	for _, c := range p.connections {
		if c.ID == "" {
			c.ID = uuid.NewString()
		}
		f.Connections = append(f.Connections, c)
	}
	flow.AutoLayout(f, unplaced)
	return f
}
