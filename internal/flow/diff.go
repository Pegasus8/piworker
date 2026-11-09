package flow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/Pegasus8/piworker/internal/node"
)

// Flow diffs (docs/design/mcp.md §10): what a draft changes compared with a
// base version, for the approval page and the low agent-deploy policy. Values
// of Sensitive properties never appear: they read as HiddenValue unless the
// value is exactly a {{secret.NAME}} reference (names are shown). The node
// and field diffs are for the editor; the alerts are also stored with a
// draft and returned to its author, so they hold only what an agent may read.

// HiddenValue replaces the value of a Sensitive property in a diff.
const HiddenValue = "<hidden>"

// Changes of a node or field.
const (
	ChangeAdded   = "added"
	ChangeRemoved = "removed"
	ChangeChanged = "changed"
)

// Alert kinds, in the order alerts on one field are listed. They are stable:
// the low agent-deploy policy and the editor match on them.
const (
	// AlertRiskRaised: the flow's risk (Risk) is higher than the base's.
	AlertRiskRaised = "risk_raised"
	// AlertPrivilegedNodeAdded: an enabled node is privileged (instance
	// risk) where the base had no enabled privileged node with its ID.
	AlertPrivilegedNodeAdded = "privileged_node_added"
	// AlertDestinationChanged: a Destination property was added, removed or
	// changed (on added and removed nodes too; enabling a node counts as
	// adding it, disabling one as removing it).
	AlertDestinationChanged = "destination_changed"
	// AlertDynamicDestination: a new or changed Destination value holds a
	// template other than {{secret.NAME}} references.
	AlertDynamicDestination = "dynamic_destination"
	// AlertNewHost: a host in a URL or host Destination value appears
	// nowhere in the base's (enabled nodes).
	AlertNewHost = "new_host"
	// AlertSecretAdded: a secret name is referenced that the base's
	// enabled nodes never reference.
	AlertSecretAdded = "secret_added"
	// AlertSecretMoved: a secret the base references is referenced at a
	// node and property where the base (an enabled node) did not.
	AlertSecretMoved = "secret_moved"
	// AlertRiskSwitchOn: a RiskSwitch property was turned on, or added on.
	AlertRiskSwitchOn = "risk_switch_on"
	// AlertSensitiveLiteral: a new or changed Sensitive value is a literal,
	// not a {{secret.NAME}} reference.
	AlertSensitiveLiteral = "sensitive_literal"
	// AlertCredentialedRequestChanged: on a node that uses a credential in
	// the base (a secret reference or a Sensitive value), an Operation
	// property changed (an HTTP method or body) or a connection into it was
	// added: the approved credential is used for something else.
	AlertCredentialedRequestChanged = "credentialed_request_changed"
	// AlertRateChanged: on a flow that reaches other services (its risk is
	// above safe), a Rate property of a running node was added, removed or
	// changed, or a running node with Rate properties was added or removed
	// (enabled or disabled): how often a trigger fires, or how many
	// messages a limiter lets through, changed. The direction is not
	// computed (a cron expression has none to compare).
	AlertRateChanged = "rate_changed"
)

var alertRank = map[string]int{
	AlertRiskRaised: 0, AlertPrivilegedNodeAdded: 1, AlertDestinationChanged: 2, AlertDynamicDestination: 3,
	AlertNewHost: 4, AlertSecretAdded: 5, AlertSecretMoved: 6, AlertRiskSwitchOn: 7, AlertSensitiveLiteral: 8,
	AlertCredentialedRequestChanged: 9, AlertRateChanged: 10,
}

// DiffReport is what Diff found.
type DiffReport struct {
	// Nodes lists the added, removed and changed nodes: next's in its
	// order, then the removed ones in base's order. Unchanged nodes are
	// omitted.
	Nodes       []NodeDiff      `json:"nodes"`
	Connections ConnectionsDiff `json:"connections"`
	RiskBefore  node.Risk       `json:"riskBefore"`
	RiskAfter   node.Risk       `json:"riskAfter"`
	Alerts      []Alert         `json:"alerts"`
}

// NodeDiff is one added, removed or changed node. A node whose only change
// is its position is "changed" with Moved and no fields.
type NodeDiff struct {
	NodeID   string      `json:"nodeId"`
	Type     string      `json:"type"`
	TypeName string      `json:"typeName"`
	Change   string      `json:"change"`
	Fields   []FieldDiff `json:"fields"`
	Moved    bool        `json:"moved"`
}

// FieldDiff is one changed field: "type", "enabled" or "config.<prop>".
// Before is absent when added, After when removed.
type FieldDiff struct {
	Path        string `json:"path"`
	Change      string `json:"change"`
	Before      any    `json:"before,omitempty"`
	After       any    `json:"after,omitempty"`
	Sensitive   bool   `json:"sensitive"`
	Destination bool   `json:"destination"`
}

// ConnectionsDiff lists the connections added and removed, compared by
// their endpoints (IDs may differ).
type ConnectionsDiff struct {
	Added   []Connection `json:"added"`
	Removed []Connection `json:"removed"`
}

// Alert is one change the approval page and the low agent-deploy policy
// flag. NodeID and Path ("config.<prop>") are empty for flow-level alerts
// (Path also for a new input of credentialed_request_changed). Alerts are
// agent-safe: Before and After hold a property's values only when agents
// may read it (AgentVisible and not Sensitive, the read views' rule), risks
// for risk_raised, the host for new_host, the secret name for the secret
// alerts and booleans for risk_switch_on; otherwise they are absent (the
// editor's field diffs have the values).
type Alert struct {
	Kind    string `json:"kind"`
	NodeID  string `json:"nodeId,omitempty"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
	Before  any    `json:"before,omitempty"`
	After   any    `json:"after,omitempty"`
}

// diffNode is a node with its schema, as Diff reads it.
type diffNode struct {
	n      *Node
	key    string // the node ID, plus its occurrence for a repeated ID
	order  int
	schema map[string]node.ConfigProperty // nil for an unregistered type
}

func (d *diffNode) prop(name string) (node.ConfigProperty, bool) {
	p, ok := d.schema[name]
	return p, ok
}

// sensitive reports whether a property's value must be hidden: Sensitive in
// the schema, or any property of an unregistered type.
func (d *diffNode) sensitive(name string) bool {
	if d == nil {
		return false
	}
	if d.schema == nil {
		return true
	}
	return d.schema[name].Sensitive
}

// live is d when it runs (enabled), nil otherwise: alerts are about the
// flow that runs, where a disabled node is absent.
func (d *diffNode) live() *diffNode {
	if d != nil && d.n.Enabled {
		return d
	}
	return nil
}

// credentialed reports whether the node uses a credential: a secret
// reference anywhere in its config, or a Sensitive value.
func (d *diffNode) credentialed() bool {
	if d == nil {
		return false
	}
	for _, key := range configKeys(d) {
		v := d.n.Config[key]
		if len(secretNames(v)) > 0 || (d.sensitive(key) && !isUnset(v)) {
			return true
		}
	}
	return false
}

// hasOperation reports whether the node's schema has an Operation property.
func (d *diffNode) hasOperation() bool {
	for _, p := range d.schema {
		if p.Operation {
			return true
		}
	}
	return false
}

// rateProps returns the names of the node's Rate properties, sorted.
func (d *diffNode) rateProps() []string {
	var names []string
	for name, p := range d.schema {
		if p.Rate {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// agentReadable reports whether an agent may read key on every one of nodes
// (nil nodes aside): declared AgentVisible and not Sensitive, the read
// views' rule. Alerts show a property's values only then.
func agentReadable(key string, nodes ...*diffNode) bool {
	seen := false
	for _, d := range nodes {
		if d == nil {
			continue
		}
		p, ok := d.prop(key)
		if !ok || !p.AgentVisible || p.Sensitive {
			return false
		}
		seen = true
	}
	return seen
}

// diffNodes indexes f's nodes by key: the ID, or for the k-th repeat of an
// ID (k ≥ 1) the ID and k, so that a duplicate is compared with the same
// occurrence of the other flow instead of disappearing.
func diffNodes(f *Flow, reg *node.Registry, offset int) (map[string]*diffNode, []*diffNode) {
	byKey := map[string]*diffNode{}
	seen := map[string]int{}
	var order []*diffNode
	for i := range f.Nodes {
		n := &f.Nodes[i]
		key := n.ID
		if k := seen[n.ID]; k > 0 {
			key = fmt.Sprintf("%s\x00%d", n.ID, k)
		}
		seen[n.ID]++
		d := &diffNode{n: n, key: key, order: offset + i}
		if info, ok := reg.Get(n.Type); ok {
			d.schema = map[string]node.ConfigProperty{}
			if info.Config != nil {
				d.schema = info.Config.Properties
			}
		}
		byKey[key] = d
		order = append(order, d)
	}
	return byKey, order
}

// Diff compares next with base (nil: an empty flow). The report is
// deterministic and never holds the value of a Sensitive property; its
// alerts hold no value an agent may not read. Nodes are paired by ID; a
// repeated ID is paired by occurrence, so a duplicate is reported as a node
// of its own.
func Diff(base, next *Flow, reg *node.Registry) DiffReport {
	if base == nil {
		base = &Flow{}
	}
	if next == nil {
		next = &Flow{}
	}
	if reg == nil {
		reg = node.NewRegistry()
	}
	report := DiffReport{
		Nodes:       []NodeDiff{},
		Connections: diffConnections(base.Connections, next.Connections),
		RiskBefore:  Risk(base, reg),
		RiskAfter:   Risk(next, reg),
	}
	baseByKey, baseOrder := diffNodes(base, reg, len(next.Nodes))
	nextByKey, nextOrder := diffNodes(next, reg, 0)

	for _, d := range nextOrder {
		if nd, changed := nodeDiff(baseByKey[d.key], d, reg); changed {
			report.Nodes = append(report.Nodes, nd)
		}
	}
	for _, d := range baseOrder {
		if nextByKey[d.key] == nil {
			nd, _ := nodeDiff(d, nil, reg)
			report.Nodes = append(report.Nodes, nd)
		}
	}
	report.Alerts = diffAlerts(report, baseByKey, nextByKey, baseOrder, nextOrder, reg)
	return report
}

// nodeDiff compares one node (b or n may be nil: added or removed).
func nodeDiff(b, n *diffNode, reg *node.Registry) (NodeDiff, bool) {
	cur := n
	if cur == nil {
		cur = b
	}
	nd := NodeDiff{NodeID: cur.n.ID, Type: cur.n.Type, Fields: []FieldDiff{}}
	if info, ok := reg.Get(cur.n.Type); ok {
		nd.TypeName = info.Name
	}
	switch {
	case b == nil:
		nd.Change = ChangeAdded
		if !n.n.Enabled {
			nd.Fields = append(nd.Fields, FieldDiff{Path: "enabled", Change: ChangeAdded, After: false})
		}
	case n == nil:
		nd.Change = ChangeRemoved
	default:
		nd.Change = ChangeChanged
		if b.n.Type != n.n.Type {
			nd.Fields = append(nd.Fields, FieldDiff{Path: "type", Change: ChangeChanged, Before: b.n.Type, After: n.n.Type})
		}
		if b.n.Enabled != n.n.Enabled {
			nd.Fields = append(nd.Fields, FieldDiff{Path: "enabled", Change: ChangeChanged, Before: b.n.Enabled, After: n.n.Enabled})
		}
		nd.Moved = b.n.Position != n.n.Position
	}
	for _, key := range configKeys(b, n) {
		bv, nv := configValue(b, key), configValue(n, key)
		if sameValue(bv, nv) {
			continue
		}
		sensitive := b.sensitive(key) || n.sensitive(key)
		fd := FieldDiff{Path: "config." + key, Sensitive: sensitive, Destination: isDestination(b, n, key)}
		switch {
		case isUnset(bv):
			fd.Change, fd.After = ChangeAdded, shownValue(nv, sensitive)
		case isUnset(nv):
			fd.Change, fd.Before = ChangeRemoved, shownValue(bv, sensitive)
		default:
			fd.Change, fd.Before, fd.After = ChangeChanged, shownValue(bv, sensitive), shownValue(nv, sensitive)
		}
		nd.Fields = append(nd.Fields, fd)
	}
	changed := nd.Change != ChangeChanged || len(nd.Fields) > 0 || nd.Moved
	return nd, changed
}

// configKeys returns the config keys of both nodes, sorted.
func configKeys(nodes ...*diffNode) []string {
	set := map[string]bool{}
	for _, d := range nodes {
		if d == nil {
			continue
		}
		for k := range d.n.Config {
			set[k] = true
		}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func configValue(d *diffNode, key string) any {
	if d == nil {
		return nil
	}
	return d.n.Config[key]
}

// isDestination reports whether key is a Destination property of either
// node's type.
func isDestination(b, n *diffNode, key string) bool {
	for _, d := range []*diffNode{b, n} {
		if d == nil {
			continue
		}
		if p, ok := d.prop(key); ok && p.Destination {
			return true
		}
	}
	return false
}

// isUnset treats null and "" as an unset value.
func isUnset(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

// sameValue compares two config values as JSON (unset values are equal).
func sameValue(a, b any) bool {
	if isUnset(a) || isUnset(b) {
		return isUnset(a) && isUnset(b)
	}
	ja, errA := json.Marshal(a)
	jb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(ja, jb)
}

// shownValue is v as a diff shows it: HiddenValue for a Sensitive value that
// is not exactly a {{secret.NAME}} reference, otherwise a copy.
func shownValue(v any, sensitive bool) any {
	if sensitive {
		if s, ok := v.(string); ok && IsSecretReference(s) {
			return s
		}
		return HiddenValue
	}
	return cloneValue(v)
}

// IsSecretReference reports whether s is exactly one {{secret.NAME}}
// reference (whitespace inside the braces allowed).
func IsSecretReference(s string) bool {
	m := placeholderRe.FindString(s)
	return m != "" && m == strings.TrimSpace(s) && isSecretPlaceholder(m)
}

// secretOnly reports whether a Sensitive value holds no literal: every
// string inside it is empty or exactly a secret reference.
func secretOnly(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return x == "" || IsSecretReference(x)
	case map[string]any:
		for _, e := range x {
			if !secretOnly(e) {
				return false
			}
		}
		return true
	case []any:
		for _, e := range x {
			if !secretOnly(e) {
				return false
			}
		}
		return true
	}
	return false
}

// secretNames returns the {{secret.NAME}} names referenced anywhere in v,
// sorted and once each.
func secretNames(v any) []string {
	var texts []string
	texts = appendLeafStrings(texts, v)
	set := map[string]bool{}
	for _, t := range texts {
		for _, m := range placeholderRe.FindAllString(t, -1) {
			if isSecretPlaceholder(m) {
				inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(m, "{{"), "}}"))
				set[strings.TrimPrefix(inner, "secret.")] = true
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// hostLikeProps hold a bare host (or host:port) rather than a URL.
var hostLikeProps = map[string]bool{"host": true, "hostname": true, "server": true}

// destinationHost returns the host a Destination value sends to: the host
// of a URL with a literal scheme and host, or a bare host[:port] in a
// host-like property; "" when there is none or it comes from a template.
func destinationHost(prop string, v any) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	rendered, _ := renderMarkers(s)
	host := ""
	if u, err := url.Parse(rendered); err == nil && u.Scheme != "" && u.Host != "" {
		host = u.Hostname()
	} else if hostLikeProps[prop] {
		host = rendered
		if h, _, err := net.SplitHostPort(rendered); err == nil {
			host = h
		}
	}
	if host == "" || strings.Contains(host, dynamicMarker) || strings.Contains(host, secretMarker) ||
		strings.ContainsAny(host, "/@ ") {
		return ""
	}
	return strings.ToLower(host)
}

// hasDynamicTemplate reports whether a value holds a placeholder other than
// {{secret.NAME}}.
func hasDynamicTemplate(v any) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	_, dynamic := renderMarkers(s)
	return dynamic
}

// diffAlerts computes the alerts of a diff, in a deterministic order. They
// describe the flow that runs: a disabled node counts as absent (enabling
// one is like adding it, disabling one like removing it). They are stored
// with a draft and returned to its author, so they show a property's values
// only when an agent may read it (agentReadable); otherwise only risks,
// hosts, secret names and risk-switch booleans.
func diffAlerts(report DiffReport, baseByKey, nextByKey map[string]*diffNode,
	baseOrder, nextOrder []*diffNode, reg *node.Registry) []Alert {
	type ranked struct {
		order int
		alert Alert
	}
	var list []ranked
	add := func(order int, a Alert) { list = append(list, ranked{order, a}) }

	if riskRank(report.RiskAfter) > riskRank(report.RiskBefore) {
		add(-1, Alert{Kind: AlertRiskRaised, Before: report.RiskBefore, After: report.RiskAfter,
			Message: fmt.Sprintf("the flow's risk rises from %s to %s", report.RiskBefore, report.RiskAfter)})
	}

	// What the base flow uses when it runs, for the "new" alerts.
	baseHosts := map[string]bool{}
	baseSecrets := map[string]bool{}
	baseSecretAt := map[[3]string]bool{} // node key, prop, name
	for _, d := range baseOrder {
		if d.live() == nil {
			continue
		}
		for _, key := range configKeys(d) {
			v := d.n.Config[key]
			if p, ok := d.prop(key); ok && p.Destination {
				if h := destinationHost(key, v); h != "" {
					baseHosts[h] = true
				}
			}
			for _, name := range secretNames(v) {
				baseSecrets[name] = true
				baseSecretAt[[3]string{d.key, key, name}] = true
			}
		}
	}

	// gone reports the destinations of a running base node that no longer
	// runs (removed, or disabled: verb says which).
	gone := func(b *diffNode, verb string) {
		for _, key := range configKeys(b) {
			if p, ok := b.prop(key); ok && p.Destination && !isUnset(b.n.Config[key]) {
				a := Alert{Kind: AlertDestinationChanged, NodeID: b.n.ID, Path: "config." + key,
					Message: fmt.Sprintf("%s node %s (%s), which sent data or connected (%s)", verb, b.n.ID, b.n.Type, key)}
				if agentReadable(key, b) {
					a.Before = cloneValue(b.n.Config[key])
				}
				add(b.order, a)
			}
		}
	}

	// Rates matter where the flow sends somewhere: a daily request turned
	// into ten a second spends the approved credentials, on the flow that
	// runs.
	ratesMatter := riskRank(report.RiskAfter) > riskRank(node.RiskSafe)
	// rateNode reports a running node with Rate properties that starts or
	// stops running (verb: adds, enables, removes, disables).
	rateNode := func(d *diffNode, verb string) {
		if props := d.rateProps(); ratesMatter && len(props) > 0 {
			add(d.order, Alert{Kind: AlertRateChanged, NodeID: d.n.ID,
				Message: fmt.Sprintf("%s node %s (%s), which sets how often messages flow (%s)", verb, d.n.ID, d.n.Type, strings.Join(props, ", "))})
		}
	}

	alertedHosts := map[string]bool{}
	alertedSecrets := map[string]bool{}
	for _, d := range nextOrder {
		b := baseByKey[d.key]
		if d.n.Enabled && instanceRisk(d.n, reg) == node.RiskPrivileged &&
			(b == nil || !b.n.Enabled || instanceRisk(b.n, reg) != node.RiskPrivileged) {
			msg := fmt.Sprintf("node %s (%s) is privileged: it acts on this device or can reach any address", d.n.ID, d.n.Type)
			if b == nil {
				msg = fmt.Sprintf("adds the privileged node %s (%s): it acts on this device or can reach any address", d.n.ID, d.n.Type)
			}
			add(d.order, Alert{Kind: AlertPrivilegedNodeAdded, NodeID: d.n.ID, Message: msg})
		}
		if d.live() == nil {
			if lb := b.live(); lb != nil {
				gone(lb, "disables")
				rateNode(lb, "disables")
			}
			continue
		}
		switch {
		case b == nil:
			rateNode(d, "adds")
		case b.live() == nil:
			rateNode(d, "enables")
		}
		b = b.live() // a node enabled here but not in base is new
		credentialed := b.credentialed()
		for _, key := range configKeys(b, d) {
			bv, nv := configValue(b, key), d.n.Config[key]
			path := "config." + key
			p, declared := d.prop(key)
			changed := !sameValue(bv, nv)
			readable := agentReadable(key, b, d)
			values := func(a Alert, before, after bool) Alert {
				if readable {
					if before && !isUnset(bv) {
						a.Before = cloneValue(bv)
					}
					if after && !isUnset(nv) {
						a.After = cloneValue(nv)
					}
				}
				return a
			}

			if isDestination(b, d, key) && changed {
				add(d.order, values(Alert{Kind: AlertDestinationChanged, NodeID: d.n.ID, Path: path,
					Message: fmt.Sprintf("changes where node %s (%s) sends data or connects (%s)", d.n.ID, d.n.Type, key)}, true, true))
			}
			if declared && p.Destination {
				if changed && hasDynamicTemplate(nv) {
					add(d.order, values(Alert{Kind: AlertDynamicDestination, NodeID: d.n.ID, Path: path,
						Message: fmt.Sprintf("node %s (%s) takes its destination (%s) from message data or variables", d.n.ID, d.n.Type, key)}, false, true))
				}
				if h := destinationHost(key, nv); h != "" && !baseHosts[h] && !alertedHosts[h] {
					alertedHosts[h] = true
					add(d.order, Alert{Kind: AlertNewHost, NodeID: d.n.ID, Path: path, After: h,
						Message: fmt.Sprintf("node %s (%s) sends to %s, a host the base version never uses", d.n.ID, d.n.Type, h)})
				}
			}
			for _, name := range secretNames(nv) {
				switch {
				case !baseSecrets[name]:
					if !alertedSecrets[name] {
						alertedSecrets[name] = true
						add(d.order, Alert{Kind: AlertSecretAdded, NodeID: d.n.ID, Path: path, After: name,
							Message: fmt.Sprintf("references the secret %s, which the base version never uses", name)})
					}
				case !baseSecretAt[[3]string{d.key, key, name}]:
					add(d.order, Alert{Kind: AlertSecretMoved, NodeID: d.n.ID, Path: path, After: name,
						Message: fmt.Sprintf("uses the secret %s in %s of node %s, where the base version does not", name, key, d.n.ID)})
				}
			}
			if declared && p.RiskSwitch && nv == true && bv != true {
				before := any(false)
				if b == nil {
					before = nil
				}
				add(d.order, Alert{Kind: AlertRiskSwitchOn, NodeID: d.n.ID, Path: path, Before: before, After: true,
					Message: fmt.Sprintf("turns on %s of node %s (%s), which widens what it may do", key, d.n.ID, d.n.Type)})
			}
			if declared && p.Sensitive && changed && !isUnset(nv) && !secretOnly(nv) {
				add(d.order, Alert{Kind: AlertSensitiveLiteral, NodeID: d.n.ID, Path: path,
					Message: fmt.Sprintf("%s of node %s holds a literal credential instead of a {{secret.NAME}} reference", key, d.n.ID)})
			}
			if credentialed && changed && isOperation(b, d, key) {
				add(d.order, values(Alert{Kind: AlertCredentialedRequestChanged, NodeID: d.n.ID, Path: path,
					Message: fmt.Sprintf("changes what node %s (%s) does with the credential it uses (%s)", d.n.ID, d.n.Type, key)}, true, true))
			}
			if ratesMatter && b != nil && changed && isRate(b, d, key) {
				add(d.order, values(Alert{Kind: AlertRateChanged, NodeID: d.n.ID, Path: path,
					Message: fmt.Sprintf("changes how often messages flow through node %s (%s) (%s)", d.n.ID, d.n.Type, key)}, true, true))
			}
		}
	}
	for _, b := range baseOrder {
		if nextByKey[b.key] == nil && b.live() != nil {
			gone(b, "removes")
			rateNode(b, "removes")
		}
	}

	// A new input into a running, credentialed request changes what the
	// credential sends.
	inputAlerted := map[string]bool{}
	for _, c := range report.Connections.Added {
		d := firstLive(nextOrder, c.TargetNode)
		if d == nil || inputAlerted[d.key] || !d.hasOperation() {
			continue
		}
		if b := baseByKey[d.key].live(); b != nil && b.credentialed() {
			inputAlerted[d.key] = true
			add(d.order, Alert{Kind: AlertCredentialedRequestChanged, NodeID: d.n.ID,
				Message: fmt.Sprintf("connects a new input into node %s (%s), which sends with a credential: what it sends changes", d.n.ID, d.n.Type)})
		}
	}

	sort.SliceStable(list, func(i, j int) bool {
		x, y := list[i], list[j]
		if x.order != y.order {
			return x.order < y.order
		}
		if x.alert.Path != y.alert.Path {
			return x.alert.Path < y.alert.Path
		}
		if alertRank[x.alert.Kind] != alertRank[y.alert.Kind] {
			return alertRank[x.alert.Kind] < alertRank[y.alert.Kind]
		}
		return x.alert.Message < y.alert.Message
	})
	out := make([]Alert, len(list))
	for i, r := range list {
		out[i] = r.alert
	}
	return out
}

// firstLive returns the running node of order that a connection to id
// reaches (the first with that ID), or nil.
func firstLive(order []*diffNode, id string) *diffNode {
	for _, d := range order {
		if d.n.ID == id {
			return d.live()
		}
	}
	return nil
}

// isOperation reports whether key is an Operation property of either node's
// type.
func isOperation(b, n *diffNode, key string) bool {
	for _, d := range []*diffNode{b, n} {
		if d == nil {
			continue
		}
		if p, ok := d.prop(key); ok && p.Operation {
			return true
		}
	}
	return false
}

// isRate reports whether key is a Rate property of either node's type.
func isRate(b, n *diffNode, key string) bool {
	for _, d := range []*diffNode{b, n} {
		if d == nil {
			continue
		}
		if p, ok := d.prop(key); ok && p.Rate {
			return true
		}
	}
	return false
}

// diffConnections compares connections by their endpoints, as multisets.
func diffConnections(base, next []Connection) ConnectionsDiff {
	key := func(c Connection) [4]string { return [4]string{c.SourceNode, c.SourcePort, c.TargetNode, c.TargetPort} }
	out := ConnectionsDiff{Added: []Connection{}, Removed: []Connection{}}
	baseCount := map[[4]string]int{}
	for _, c := range base {
		baseCount[key(c)]++
	}
	nextCount := map[[4]string]int{}
	for _, c := range next {
		nextCount[key(c)]++
	}
	seen := map[[4]string]int{}
	for _, c := range next {
		k := key(c)
		seen[k]++
		if seen[k] > baseCount[k] {
			out.Added = append(out.Added, c)
		}
	}
	seen = map[[4]string]int{}
	for _, c := range base {
		k := key(c)
		seen[k]++
		if seen[k] > nextCount[k] {
			out.Removed = append(out.Removed, c)
		}
	}
	return out
}
