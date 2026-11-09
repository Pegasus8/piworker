package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Pegasus8/piworker/internal/flow"
	"github.com/Pegasus8/piworker/internal/node"
)

// What agents see of what they author (docs/design/mcp.md §10) never depends
// on how what they sent compares with values they may not read: an answer
// that hid text equal to a secret's value, or to a credential the admin
// typed, would let them confirm guesses one at a time. What an agent wrote
// comes back as it wrote it. The admin's content it may not read is withheld
// wholesale instead: the settings it did not write (kept) that are hidden
// (hiddenSetting) or hold a known value (what the read views redact), and
// whether a setting is withheld depends on that content alone.

// knownRedactor redacts the values the read views redact from what agents
// see of f: the secret store's values and the credentials typed literally
// into f's Sensitive settings.
func (a *Authoring) knownRedactor(f *flow.Flow) secretRedactor {
	values := a.views.secretValues()
	if f != nil {
		values = append(values, a.views.sensitiveValues(f)...)
	}
	return valuesRedactor(values)
}

// holdsKnown reports whether v holds a value red redacts.
func holdsKnown(red secretRedactor, v any) bool {
	return red.r != nil && !sameJSON(redactValue(red, v), v)
}

// withheldSettings lists the settings of next that hold the admin's content
// agents may not read: kept (proposal.kept: the agent did not write them)
// and hidden, or whose base value holds a known value (knownRedactor of the
// base). What the agent wrote is never among them; a merged object counts
// by the base's part.
func (a *Authoring) withheldSettings(base, next *flow.Flow, kept map[propKey]bool) map[propKey]bool {
	red := a.knownRedactor(base)
	out := map[propKey]bool{}
	for k := range kept {
		n := next.GetNode(k.node)
		if n == nil {
			continue
		}
		if a.hiddenSetting(n, k.prop) {
			out[k] = true
			continue
		}
		v := n.Config[k.prop]
		if b := baseNode(base, n); b != nil {
			v = b.Config[k.prop]
		}
		if holdsKnown(red, v) {
			out[k] = true
		}
	}
	return out
}

// storedWithheld lists the withheld settings of a stored flow, all of which
// are the admin's.
func (a *Authoring) storedWithheld(f *flow.Flow) map[propKey]bool {
	all := map[propKey]bool{}
	for i := range f.Nodes {
		for prop := range f.Nodes[i].Config {
			all[propKey{f.Nodes[i].ID, prop}] = true
		}
	}
	return a.withheldSettings(f, f, all)
}

// storedRedaction is DryRunInput.Redact for a stored flow: its hidden
// settings and those holding a known value never reach an agent's run.
func (a *Authoring) storedRedaction(f *flow.Flow) func(n *flow.Node, prop string) bool {
	red := a.knownRedactor(f)
	return func(n *flow.Node, prop string) bool {
		return a.hiddenSetting(n, prop) || holdsKnown(red, n.Config[prop])
	}
}

// keptSettings is what SaveDraft stores with a draft (storage KeptJSON):
// the settings withheld from its author (withheldSettings), which a dry-run
// of the draft hides from the run and its view shows as "<redacted>", and
// whether the flow name is the base's and holds a known value.
type keptSettings struct {
	Props [][2]string `json:"props"` // [node ID, property], sorted
	Name  bool        `json:"name,omitempty"`
}

func newKeptSettings(withheld map[propKey]bool, name bool) keptSettings {
	out := keptSettings{Props: [][2]string{}, Name: name}
	for _, k := range sortedPropKeys(withheld) {
		out.Props = append(out.Props, [2]string{k.node, k.prop})
	}
	return out
}

// readKept decodes a draft's kept settings (ok false: the draft has no
// readable list).
func readKept(raw json.RawMessage) (keptSettings, map[propKey]bool, bool) {
	var kept keptSettings
	if err := json.Unmarshal(raw, &kept); err != nil || kept.Props == nil {
		return keptSettings{}, nil, false
	}
	set := make(map[propKey]bool, len(kept.Props))
	for _, k := range kept.Props {
		set[propKey{k[0], k[1]}] = true
	}
	return kept, set, true
}

// draftWithheld lists the withheld settings of a stored draft of an existing
// flow (proposed: its flow): the stored list, or, without a readable one,
// every setting the stored-flow rule withholds.
func (a *Authoring) draftWithheld(raw json.RawMessage, proposed *flow.Flow) map[propKey]bool {
	if _, set, ok := readKept(raw); ok {
		return set
	}
	return a.storedWithheld(proposed)
}

// keptRedaction is DryRunInput.Redact for a draft of an existing flow: the
// settings withheld from its author (stored with the draft), never what it
// wrote. A draft without a readable list hides what the stored-flow rule
// hides.
func (a *Authoring) keptRedaction(raw json.RawMessage, proposed *flow.Flow) func(n *flow.Node, prop string) bool {
	if _, set, ok := readKept(raw); ok {
		return func(n *flow.Node, prop string) bool { return set[propKey{n.ID, prop}] }
	}
	return a.storedRedaction(proposed)
}

// settingFreeIssueCodes are the issue codes whose messages never quote a
// node's settings (IDs, types, ports, positions, or the service's own fixed
// texts and the secret names the agent wrote).
var settingFreeIssueCodes = map[string]bool{
	flow.IssueEmptyNodeID: true, flow.IssueDuplicateNodeID: true, flow.IssueNoTrigger: true,
	flow.IssueDuplicateConnectionID: true, flow.IssueConnectionUnknownNode: true, flow.IssueConnectionUnknownPort: true,
	flow.IssueSelfLoop: true, flow.IssueCycle: true, flow.IssueUnknownType: true, flow.IssueTriggerNotConnected: true,
	flow.IssueInvalidPosition: true, flow.IssueHiddenSettings: true,
	IssueSensitiveLiteral: true, IssueUnresolvedRedaction: true, IssueUnknownSecret: true,
}

// agentIssues renders issues of f for an agent. A message about a node
// holding withheld settings may quote them (Lint hides only what the read
// views hide, by comparing values), so it is replaced by a fixed text,
// except a config_type or undefined_secret issue of a setting the agent
// wrote, whose message is built again from that setting alone. Nothing else
// is redacted by comparison: the rest quotes what the agent wrote or may
// read. URLs are cut after their host, messages bounded, duplicates
// dropped.
func (a *Authoring) agentIssues(f *flow.Flow, withheld map[propKey]bool, issues []flow.Issue) []flow.Issue {
	nodes := map[string]bool{}
	for k := range withheld {
		nodes[k.node] = true
	}
	out := make([]flow.Issue, 0, len(issues))
	seen := map[flow.Issue]bool{}
	for _, is := range issues {
		if id, prop, ok := issueSetting(is.Path); ok && nodes[id] && !settingFreeIssueCodes[is.Code] {
			is.Message = a.withheldIssueMessage(f, id, prop, withheld, is.Code)
		}
		is.Path = capText(is.Path, 4*maxViewIDBytes)
		is.Message = capText(hideURLsInText(is.Message), maxAgentIssueBytes)
		if seen[is] {
			continue
		}
		seen[is] = true
		out = append(out, is)
	}
	return out
}

// issueSetting parses an issue path at a node ("/nodes/{id}" or
// "/nodes/{id}/config/{prop}"): the node ID and the property ("" for the
// node).
func issueSetting(path string) (id, prop string, ok bool) {
	rest, ok := strings.CutPrefix(path, "/nodes/")
	if !ok {
		return "", "", false
	}
	seg, tail, _ := strings.Cut(rest, "/")
	if p, isProp := strings.CutPrefix(tail, "config/"); isProp {
		prop = pointerUnescape(p)
	}
	return pointerUnescape(seg), prop, true
}

var pointerUnescaper = strings.NewReplacer("~1", "/", "~0", "~")

func pointerUnescape(s string) string { return pointerUnescaper.Replace(s) }

// withheldIssueMessage is the message an agent gets for an issue of code at
// node id (prop: "" for the node) of f, a node holding withheld settings.
func (a *Authoring) withheldIssueMessage(f *flow.Flow, id, prop string, withheld map[propKey]bool, code string) string {
	n := f.GetNode(id)
	if n != nil && prop != "" && !withheld[propKey{id, prop}] {
		switch code {
		case flow.IssueConfigType:
			if schema := a.schemaOf(n.Type); schema != nil {
				if p, ok := schema.Properties[prop]; ok {
					one := &node.ConfigSchema{Properties: map[string]node.ConfigProperty{prop: p}}
					if found := node.CheckConfig(one, map[string]any{prop: n.Config[prop]}); len(found) == 1 {
						return found[0].Message
					}
				}
			}
		case flow.IssueUndefinedSecret:
			var names []string
			for _, name := range referencedSecrets(n.Config[prop]) {
				if _, ok := a.secrets.Get(name); !ok {
					names = append(names, fmt.Sprintf("%q", capText(name, maxViewIDBytes)))
				}
			}
			if len(names) > 0 {
				return fmt.Sprintf("config.%s references secrets that are not defined: %s", prop, strings.Join(names, ", "))
			}
		}
	}
	if code == flow.IssueUndefinedSecret {
		return "a setting of this node you cannot read references a secret that is not defined: the admin must define it"
	}
	return fmt.Sprintf("this node's settings are not valid (%s); the details are not shown: they may quote settings of the node you cannot read", code)
}

// valueFreeAlertKinds are the alert kinds whose messages name no value (IDs,
// types and property names only).
var valueFreeAlertKinds = map[string]bool{
	flow.AlertRiskRaised: true, flow.AlertPrivilegedNodeAdded: true, flow.AlertDestinationChanged: true,
	flow.AlertDynamicDestination: true, flow.AlertRiskSwitchOn: true, flow.AlertSensitiveLiteral: true,
	flow.AlertCredentialedRequestChanged: true, flow.AlertRateChanged: true,
	flow.AlertGlobalVariableWrite: true, flow.AlertGlobalVariableRead: true,
}

// agentAlerts renders flow.Diff's alerts of a proposal on base for an agent.
// An alert about a withheld setting carries no values and a message that
// names none (a host or secret name in a setting the agent cannot read and
// did not write stays unknown, H7). Before values are the base's and are
// redacted as the read views redact them; After values of other settings
// are what the agent wrote or may read and are shown as they are.
func (a *Authoring) agentAlerts(base *flow.Flow, withheld map[propKey]bool, alerts []flow.Alert) []flow.Alert {
	red := a.knownRedactor(base)
	out := make([]flow.Alert, len(alerts))
	for i, al := range alerts {
		if prop := alertSetting(al); prop != "" && withheld[propKey{al.NodeID, prop}] {
			al.Before, al.After = nil, nil
			if !valueFreeAlertKinds[al.Kind] {
				al.Message = fmt.Sprintf("node %s: %s, in %s, a setting you cannot read (the admin sees the value)",
					al.NodeID, strings.ReplaceAll(al.Kind, "_", " "), prop)
			}
		}
		if al.Before != nil {
			al.Before = visibleValue(red, al.Before)
		}
		if al.After != nil {
			al.After = visibleValue(secretRedactor{}, al.After)
		}
		al.NodeID = capText(al.NodeID, maxViewIDBytes)
		al.Message = capText(hideURLsInText(al.Message), maxAgentIssueBytes)
		out[i] = al
	}
	return out
}

// alertSetting is the property an alert's values come from ("" for none).
func alertSetting(al flow.Alert) string {
	if al.NodeID == "" {
		return ""
	}
	if prop, ok := strings.CutPrefix(al.Path, "config."); ok {
		return prop
	}
	if al.Kind == flow.AlertGlobalVariableWrite {
		return "key"
	}
	return ""
}

// draftConfigView is a draft's node config as its author sees it: hidden
// and withheld settings as "<redacted>", the rest bounded as written (no
// value is redacted by comparison).
func draftConfigView(schema *node.ConfigSchema, nodeID string, cfg map[string]any, withheld map[propKey]bool) map[string]any {
	out := make(map[string]any, len(cfg))
	keys := make([]string, 0, len(cfg))
	for k := range cfg {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		key := capText(k, maxConfigKeyBytes)
		p, declared := node.ConfigProperty{}, false
		if schema != nil {
			p, declared = schema.Properties[k]
		}
		if !declared || !p.AgentVisible || p.Sensitive || withheld[propKey{nodeID, k}] {
			out[key] = redactedMark
			continue
		}
		out[key] = visibleValue(secretRedactor{}, cfg[k])
	}
	return out
}
