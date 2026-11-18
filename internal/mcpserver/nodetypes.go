package mcpserver

import (
	"cmp"
	"slices"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
)

// NodeTypeLister lists the registered node types (*node.Registry).
type NodeTypeLister interface {
	List() []node.NodeTypeInfo
}

// Bounds of pw_list_node_types: each type's documentation is cut to
// maxNodeTypeDocBytes, and the list's JSON stays within
// maxNodeTypeListBytes (the read views' bound: a result carries it twice).
const (
	maxNodeTypeDocBytes  = 8 << 10
	maxNodeTypeListBytes = 64 << 10
)

// NodeTypeList is what pw_list_node_types answers: registry metadata only,
// never a flow or a secret.
type NodeTypeList struct {
	// Types are ordered by category (input, processing, output) and type.
	Types []NodeTypeView `json:"types"`
	// Total counts the types matching the filters, listed or not.
	Total int `json:"total"`
	// Truncated: types or documentation were left out, or documentation
	// cut, to keep the list within maxNodeTypeListBytes. Documentation a
	// list without filters omits does not count.
	Truncated bool `json:"truncated"`
}

// NodeTypeView is one node type as agents author with it.
type NodeTypeView struct {
	Type        string         `json:"type"`
	Name        string         `json:"name"`
	Category    string         `json:"category"`
	Risk        node.Risk      `json:"risk"`
	Description string         `json:"description"`
	Inputs      []NodePortView `json:"inputs"`
	Outputs     []NodePortView `json:"outputs"`
	Config      NodeConfigView `json:"config"`
	// Documentation is the type's markdown documentation, cut to
	// maxNodeTypeDocBytes.
	Documentation string `json:"documentation,omitempty"`
	// DocumentationOmitted: left out of a list without filters, or to keep
	// the list small (ask for the category or the type alone).
	DocumentationOmitted bool `json:"documentationOmitted,omitempty"`
}

// NodePortView is a port of a node type.
type NodePortView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Multiple bool   `json:"multiple"`
	Required bool   `json:"required"`
}

// NodeConfigView is a node type's config schema.
type NodeConfigView struct {
	Properties map[string]NodePropertyView `json:"properties"`
	Required   []string                    `json:"required"`
}

// NodePropertyView is one setting of a node type, with the markers that
// say how PiWorker treats it (present only when true): sensitive (a
// credential: drafts may set only a {{secret.NAME}} reference),
// agentVisible (shown to agents; other values read "<redacted>"),
// destination (where the node sends data or connects), riskSwitch (an
// opt-in that widens what the node may do; agents may not turn it on),
// operation (what selects the action a request performs), rate (how often a
// trigger fires or how many messages a node lets through).
type NodePropertyView struct {
	Type            string   `json:"type,omitempty"`
	Title           string   `json:"title,omitempty"`
	Description     string   `json:"description,omitempty"`
	Default         any      `json:"default,omitempty"`
	Enum            []string `json:"enum,omitempty"`
	Minimum         *float64 `json:"minimum,omitempty"`
	Maximum         *float64 `json:"maximum,omitempty"`
	Unit            string   `json:"unit,omitempty"`
	CaseInsensitive bool     `json:"caseInsensitive,omitempty"`
	Sensitive       bool     `json:"sensitive,omitempty"`
	AgentVisible    bool     `json:"agentVisible,omitempty"`
	Destination     bool     `json:"destination,omitempty"`
	RiskSwitch      bool     `json:"riskSwitch,omitempty"`
	Operation       bool     `json:"operation,omitempty"`
	Rate            bool     `json:"rate,omitempty"`
}

// omittedMarker is what a type whose documentation was left out carries.
const omittedMarker = `,"documentationOmitted":true`

// categoryOrder orders the categories as a flow reads: triggers first.
func categoryOrder(c types.NodeCategory) int {
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

// listNodeTypes lists the registered types matching category and typ ("":
// any), ordered by category and type, within maxNodeTypeListBytes: every
// type's structure first, in order while it fits, then the documentation in
// order while it fits (documentationOmitted otherwise), each cut to
// maxNodeTypeDocBytes. Without a filter it lists the structures only, every
// documentation omitted, so the whole registry fits (ADR-0024): an agent
// asks for a category or a type to read the documentation.
func listNodeTypes(reg NodeTypeLister, category, typ string) NodeTypeList {
	withDocs := category != "" || typ != ""
	var infos []node.NodeTypeInfo
	for _, info := range reg.List() {
		if (category == "" || string(info.Category) == category) && (typ == "" || info.Type == typ) {
			infos = append(infos, info)
		}
	}
	slices.SortFunc(infos, func(a, b node.NodeTypeInfo) int {
		return cmp.Or(cmp.Compare(categoryOrder(a.Category), categoryOrder(b.Category)), cmp.Compare(a.Type, b.Type))
	})
	list := NodeTypeList{Types: []NodeTypeView{}, Total: len(infos)}
	// The wrapper, generously: {"types":[],"total":N,"truncated":true}.
	used := 64
	for _, info := range infos {
		v := nodeTypeView(info)
		size := encodedSize(v) + 1 // and a comma
		if info.Documentation != "" {
			size += len(omittedMarker) // the least its documentation takes
		}
		if used+size > maxNodeTypeListBytes {
			list.Truncated = true
			break
		}
		used += size
		list.Types = append(list.Types, v)
	}
	for i := range list.Types {
		doc := infos[i].Documentation
		if doc == "" {
			continue
		}
		if !withDocs {
			list.Types[i].DocumentationOmitted = true // asked for, not cut: not truncated
			continue
		}
		cut := shorten(doc, maxNodeTypeDocBytes)
		if cut != doc {
			list.Truncated = true
		}
		// The documentation instead of the marker reserved for it.
		extra := len(`,"documentation":`) + encodedSize(cut) - len(omittedMarker)
		if used+extra > maxNodeTypeListBytes {
			list.Types[i].DocumentationOmitted = true
			list.Truncated = true
			continue
		}
		used += extra
		list.Types[i].Documentation = cut
	}
	// The sizes above are estimates of the parts; the whole decides.
	for len(list.Types) > 0 && encodedSize(list) > maxNodeTypeListBytes {
		list.Truncated = true
		last := &list.Types[len(list.Types)-1]
		if last.Documentation != "" {
			last.Documentation, last.DocumentationOmitted = "", true
			continue
		}
		list.Types = list.Types[:len(list.Types)-1]
	}
	return list
}

// nodeTypeView is a type without its documentation.
func nodeTypeView(info node.NodeTypeInfo) NodeTypeView {
	risk := info.Risk
	if !risk.Valid() {
		risk = node.RiskPrivileged // what flow.Risk assumes of what it cannot rate
	}
	v := NodeTypeView{
		Type: info.Type, Name: info.Name, Category: string(info.Category), Risk: risk, Description: info.Description,
		Inputs: ports(info.Inputs), Outputs: ports(info.Outputs),
		Config: NodeConfigView{Properties: map[string]NodePropertyView{}, Required: []string{}},
	}
	if info.Config != nil {
		v.Config.Required = append(v.Config.Required, info.Config.Required...)
		for name, p := range info.Config.Properties {
			v.Config.Properties[name] = NodePropertyView{
				Type: p.Type, Title: p.Title, Description: p.Description, Default: p.Default,
				Enum: slices.Clone(p.Enum), Minimum: p.Minimum, Maximum: p.Maximum, Unit: p.Unit, CaseInsensitive: p.CaseInsensitive,
				Sensitive: p.Sensitive, AgentVisible: p.AgentVisible, Destination: p.Destination, RiskSwitch: p.RiskSwitch, Operation: p.Operation, Rate: p.Rate,
			}
		}
	}
	return v
}

func ports(in []types.Port) []NodePortView {
	out := make([]NodePortView, 0, len(in))
	for _, p := range in {
		out = append(out, NodePortView{ID: p.ID, Name: p.Name, DataType: string(p.DataType), Multiple: p.Multiple, Required: p.Required})
	}
	return out
}

// encodedSize is the length of v's JSON as results encode it.
func encodedSize(v any) int {
	data, err := marshalNoEscape(v)
	if err != nil {
		return maxNodeTypeListBytes + 1 // cannot be listed
	}
	return len(data)
}
