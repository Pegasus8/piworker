// Package zigbee speaks Zigbee2MQTT's MQTT API: its topics, device names,
// availability, bridge messages and the device list with each device's
// features ("exposes"). The Zigbee nodes and the discovery service build on
// it (docs/design/zigbee.md).
package zigbee

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaxDeviceNameLength bounds a device's friendly name.
const MaxDeviceNameLength = 256

// DeviceTopic is where Zigbee2MQTT publishes a device's state.
func DeviceTopic(base, device string) string { return base + "/" + device }

// SetTopic is where a device takes commands.
func SetTopic(base, device string) string { return DeviceTopic(base, device) + "/set" }

// GetTopic is where a device is asked for a fresh reading.
func GetTopic(base, device string) string { return DeviceTopic(base, device) + "/get" }

// AvailabilityTopic is where Zigbee2MQTT publishes whether a device is online.
func AvailabilityTopic(base, device string) string {
	return DeviceTopic(base, device) + "/availability"
}

// BridgeTopic is a topic under the bridge (state, info, devices, request/…).
func BridgeTopic(base, sub string) string { return base + "/bridge/" + sub }

// ValidDeviceName checks a device (or group) friendly name as a topic part:
// not empty, no wildcards, no empty level, not the bridge and not ending in
// a command suffix (set, get, availability).
func ValidDeviceName(name string) error {
	if name == "" {
		return fmt.Errorf("device is required")
	}
	if len(name) > MaxDeviceNameLength {
		return fmt.Errorf("device name is longer than %d bytes", MaxDeviceNameLength)
	}
	if strings.ContainsAny(name, "+#\x00") {
		return fmt.Errorf("device name cannot contain + or #")
	}
	levels := strings.Split(name, "/")
	for _, l := range levels {
		if l == "" {
			return fmt.Errorf("device name has an empty level (a leading, trailing or double /)")
		}
	}
	if strings.EqualFold(levels[0], "bridge") {
		return fmt.Errorf("the bridge is not a device: its requests are an administrator's")
	}
	switch levels[len(levels)-1] {
	case "set", "get", "availability":
		return fmt.Errorf("device name cannot end in /%s", levels[len(levels)-1])
	}
	return nil
}

// ParseAvailability reads an availability payload: {"state":"online"} or,
// in Zigbee2MQTT's legacy format, the plain word.
func ParseAvailability(payload []byte) (online, ok bool) {
	word := strings.TrimSpace(string(payload))
	var obj struct {
		State string `json:"state"`
	}
	if strings.HasPrefix(word, "{") {
		if json.Unmarshal(payload, &obj) != nil {
			return false, false
		}
		word = obj.State
	}
	switch word {
	case "online":
		return true, true
	case "offline":
		return false, true
	}
	return false, false
}

// Feature is one property of a device: what it publishes and, per its
// access bits, whether it can be set or read on request.
type Feature struct {
	Property    string   `json:"property"`
	Name        string   `json:"name"`
	Type        string   `json:"type"` // binary, numeric, enum, text, …
	Access      int      `json:"access"`
	Unit        string   `json:"unit,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	Values      []string `json:"values,omitempty"`
	ValueOn     any      `json:"valueOn,omitempty"`
	ValueOff    any      `json:"valueOff,omitempty"`
	ValueToggle any      `json:"valueToggle,omitempty"`
	Description string   `json:"description,omitempty"`
}

// Access bits of a feature.
const (
	AccessPublished = 1
	AccessSet       = 2
	AccessGet       = 4
)

// Published reports whether the feature appears in the device's state.
func (f Feature) Published() bool { return f.Access&AccessPublished != 0 }

// Settable reports whether the feature can be set.
func (f Feature) Settable() bool { return f.Access&AccessSet != 0 }

// Gettable reports whether the feature can be read on request.
func (f Feature) Gettable() bool { return f.Access&AccessGet != 0 }

// Device is a device of the network (the coordinator left out).
type Device struct {
	FriendlyName string    `json:"friendlyName"`
	IEEE         string    `json:"ieee"`
	Type         string    `json:"type"` // Router or EndDevice
	PowerSource  string    `json:"powerSource,omitempty"`
	Model        string    `json:"model,omitempty"`
	Vendor       string    `json:"vendor,omitempty"`
	Description  string    `json:"description,omitempty"`
	Supported    bool      `json:"supported"`
	Disabled     bool      `json:"disabled,omitempty"`
	Features     []Feature `json:"features"`
}

// Feature returns the device's feature with that property, or nil.
func (d Device) Feature(property string) *Feature {
	for i := range d.Features {
		if d.Features[i].Property == property {
			return &d.Features[i]
		}
	}
	return nil
}

// Properties are the properties the device publishes, in its order.
func (d Device) Properties() []string {
	var out []string
	for _, f := range d.Features {
		if f.Published() {
			out = append(out, f.Property)
		}
	}
	return out
}

// SettableProperties are the properties the device can be set, in its order.
func (d Device) SettableProperties() []string {
	var out []string
	for _, f := range d.Features {
		if f.Settable() {
			out = append(out, f.Property)
		}
	}
	return out
}

type rawExpose struct {
	Type        string      `json:"type"`
	Name        string      `json:"name"`
	Property    string      `json:"property"`
	Access      int         `json:"access"`
	Unit        string      `json:"unit"`
	ValueMin    *float64    `json:"value_min"`
	ValueMax    *float64    `json:"value_max"`
	Values      []any       `json:"values"`
	ValueOn     any         `json:"value_on"`
	ValueOff    any         `json:"value_off"`
	ValueToggle any         `json:"value_toggle"`
	Description string      `json:"description"`
	Features    []rawExpose `json:"features"`
}

type rawDevice struct {
	IEEE         string `json:"ieee_address"`
	Type         string `json:"type"`
	FriendlyName string `json:"friendly_name"`
	PowerSource  string `json:"power_source"`
	ModelID      string `json:"model_id"`
	Supported    bool   `json:"supported"`
	Disabled     bool   `json:"disabled"`
	Definition   *struct {
		Model       string      `json:"model"`
		Vendor      string      `json:"vendor"`
		Description string      `json:"description"`
		Exposes     []rawExpose `json:"exposes"`
	} `json:"definition"`
}

// ParseDevices reads bridge/devices: every device but the coordinator, its
// features flattened (a light's or switch's features are listed as its own;
// composite features keep their property, without sub-features).
func ParseDevices(payload []byte) ([]Device, error) {
	var raw []rawDevice
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("bridge/devices is not a device list: %w", err)
	}
	out := make([]Device, 0, len(raw))
	for _, r := range raw {
		if r.Type == "Coordinator" || r.FriendlyName == "" {
			continue
		}
		d := Device{FriendlyName: r.FriendlyName, IEEE: r.IEEE, Type: r.Type, PowerSource: r.PowerSource,
			Model: r.ModelID, Supported: r.Supported, Disabled: r.Disabled, Features: []Feature{}}
		if r.Definition != nil {
			d.Model, d.Vendor, d.Description = r.Definition.Model, r.Definition.Vendor, r.Definition.Description
			flatten(r.Definition.Exposes, &d.Features)
		}
		out = append(out, d)
	}
	return out, nil
}

func flatten(exposes []rawExpose, out *[]Feature) {
	for _, e := range exposes {
		if e.Property == "" {
			flatten(e.Features, out) // a light, switch, cover, …: its features
			continue
		}
		f := Feature{Property: e.Property, Name: e.Name, Type: e.Type, Access: e.Access, Unit: e.Unit,
			Min: e.ValueMin, Max: e.ValueMax, ValueOn: e.ValueOn, ValueOff: e.ValueOff, ValueToggle: e.ValueToggle,
			Description: e.Description}
		for _, v := range e.Values {
			if s, ok := v.(string); ok {
				f.Values = append(f.Values, s)
			}
		}
		*out = append(*out, f)
	}
}

// BridgeInfo is what bridge/info says that the editor shows.
type BridgeInfo struct {
	Version    string `json:"version"`
	PermitJoin bool   `json:"permitJoin"`
	// PermitJoinEnd is when joining closes, in Unix milliseconds
	// (Zigbee2MQTT 2.x), if it says.
	PermitJoinEnd *int64 `json:"permitJoinEnd,omitempty"`
	Coordinator   string `json:"coordinator,omitempty"`
}

// ParseBridgeInfo reads bridge/info.
func ParseBridgeInfo(payload []byte) (BridgeInfo, error) {
	var raw struct {
		Version       string `json:"version"`
		PermitJoin    bool   `json:"permit_join"`
		PermitJoinEnd *int64 `json:"permit_join_end"`
		Coordinator   struct {
			Type string `json:"type"`
		} `json:"coordinator"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return BridgeInfo{}, fmt.Errorf("bridge/info is not JSON: %w", err)
	}
	return BridgeInfo{Version: raw.Version, PermitJoin: raw.PermitJoin, PermitJoinEnd: raw.PermitJoinEnd, Coordinator: raw.Coordinator.Type}, nil
}
