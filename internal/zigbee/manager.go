package zigbee

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/mqttpool"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/rs/zerolog/log"
)

// Bounds of what a watcher keeps.
const (
	maxStateBytes   = 64 << 10 // one device report
	maxDevicesBytes = 4 << 20  // bridge/devices
	maxTracked      = 1000     // devices with a state or availability
	// MaxPermitJoinSeconds is Zigbee2MQTT's longest permit-join window.
	MaxPermitJoinSeconds = 254
)

// Default is the process-wide manager (main starts it); expressions read
// device states from it. Nil when not started.
var Default *Manager

// Manager watches each broker connection's Zigbee2MQTT network (<base>/#,
// on its own pooled connection, ADR-0025): the bridge's state and info, the
// device list, and each device's last state and availability. It serves
// device discovery in the editor and the state the expressions read.
type Manager struct {
	store *brokers.Store

	mu       sync.Mutex
	watchers map[string]*watcher
	stopped  bool
}

// NewManager creates a manager for the connections of store.
func NewManager(store *brokers.Store) *Manager {
	return &Manager{store: store, watchers: map[string]*watcher{}}
}

// Start watches every connection and follows the store's changes.
func (m *Manager) Start() {
	m.store.OnChange(m.sync)
	for _, name := range m.store.Names() {
		m.sync(name)
	}
}

// Stop ends every watcher.
func (m *Manager) Stop() {
	m.store.OnChange(nil)
	m.mu.Lock()
	m.stopped = true
	ws := m.watchers
	m.watchers = map[string]*watcher{}
	m.mu.Unlock()
	for _, w := range ws {
		w.stop()
	}
}

// sync (re)starts the watcher of the connection name, or stops it when the
// connection is gone.
func (m *Manager) sync(name string) {
	conn, defined := m.store.Get(name)
	m.mu.Lock()
	old := m.watchers[name]
	delete(m.watchers, name)
	var w *watcher
	if defined && !m.stopped {
		w = newWatcher(conn)
		m.watchers[name] = w
	}
	m.mu.Unlock()
	if old != nil {
		old.stop()
	}
	if w != nil {
		w.start()
	}
}

func (m *Manager) watcher(name string) (*watcher, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.watchers[name]
	return w, ok
}

// BridgeStatus is the bridge as the editor shows it.
type BridgeStatus struct {
	Online bool       `json:"online"`
	Info   BridgeInfo `json:"info"`
}

// DeviceStatus is a device with what it last reported.
type DeviceStatus struct {
	Device
	State    map[string]any `json:"state,omitempty"`
	Online   *bool          `json:"online,omitempty"`
	LastSeen *time.Time     `json:"lastSeen,omitempty"`
}

// Network is a connection's Zigbee network.
type Network struct {
	Connection string         `json:"connection"`
	BaseTopic  string         `json:"baseTopic"`
	Connected  bool           `json:"connected"` // to the broker
	Bridge     BridgeStatus   `json:"bridge"`
	Devices    []DeviceStatus `json:"devices"`
}

// Network returns the network of the connection name.
func (m *Manager) Network(name string) (Network, bool) {
	w, ok := m.watcher(name)
	if !ok {
		return Network{}, false
	}
	return w.network(), true
}

// DeviceState returns the last state device reported on the connection
// name (a copy).
func (m *Manager) DeviceState(name, device string) (map[string]any, bool) {
	w, ok := m.watcher(name)
	if !ok {
		return nil, false
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.states[device]
	if !ok || s.state == nil {
		return nil, false
	}
	return cloneState(s.state), true
}

// Connections returns the names of the watched connections.
func (m *Manager) Connections() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.watchers))
	for n := range m.watchers {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// PermitJoin opens (seconds > 0) or closes (0) the network to new devices
// for up to MaxPermitJoinSeconds. It is the administrator's: no node or
// agent tool calls it.
func (m *Manager) PermitJoin(name string, seconds int) error {
	if seconds < 0 || seconds > MaxPermitJoinSeconds {
		return fmt.Errorf("seconds must be between 0 and %d", MaxPermitJoinSeconds)
	}
	w, ok := m.watcher(name)
	if !ok {
		return fmt.Errorf("broker connection %q is not defined", name)
	}
	conn := w.sub.Conn()
	ctx, cancel := context.WithTimeout(context.Background(), mqttpool.ConnectTimeout)
	defer cancel()
	if err := conn.WaitUp(ctx, mqttpool.ConnectTimeout); err != nil {
		return fmt.Errorf("the broker is not reachable: %w", err)
	}
	body, _ := json.Marshal(struct {
		Time  int  `json:"time"`
		Value bool `json:"value"` // Zigbee2MQTT 1.x
	}{seconds, seconds > 0})
	tok := conn.Client().Publish(BridgeTopic(w.conn.BaseTopic, "request/permit_join"), 1, false, body)
	if !tok.WaitTimeout(mqttpool.ConnectTimeout) {
		return fmt.Errorf("the broker did not acknowledge the request")
	}
	return tok.Error()
}

type deviceState struct {
	state    map[string]any
	online   *bool
	lastSeen time.Time
}

// watcher follows one connection.
type watcher struct {
	conn brokers.Connection
	sub  *mqttpool.Sub

	mu      sync.Mutex
	online  bool
	info    BridgeInfo
	devices []Device
	states  map[string]*deviceState
}

func newWatcher(conn brokers.Connection) *watcher {
	return &watcher{conn: conn, states: map[string]*deviceState{}}
}

func (w *watcher) start() {
	sub, _ := mqttpool.Subscribe(w.conn.Endpoint(context.Background()), w.conn.BaseTopic+"/#", 0,
		func(msg mqtt.Message) { w.handle(msg.Topic(), msg.Payload()) }, mqttpool.RetryInterval)
	w.mu.Lock()
	w.sub = sub
	w.mu.Unlock()
}

func (w *watcher) stop() {
	w.mu.Lock()
	sub := w.sub
	w.mu.Unlock()
	if sub != nil {
		sub.Release()
	}
}

// handle files a message under the bridge or a device.
func (w *watcher) handle(topic string, payload []byte) {
	rest, ok := strings.CutPrefix(topic, w.conn.BaseTopic+"/")
	if !ok || rest == "" {
		return
	}
	if sub, isBridge := strings.CutPrefix(rest, "bridge/"); isBridge {
		w.handleBridge(sub, payload)
		return
	}
	if device, ok := strings.CutSuffix(rest, "/availability"); ok {
		online, valid := ParseAvailability(payload)
		if valid {
			w.update(device, func(s *deviceState) { s.online = &online })
		}
		return
	}
	if strings.HasSuffix(rest, "/set") || strings.HasSuffix(rest, "/get") || strings.Contains(rest, "/set/") {
		return // commands, not reports
	}
	if len(payload) > maxStateBytes {
		return
	}
	var state map[string]any
	if json.Unmarshal(payload, &state) != nil || state == nil {
		return
	}
	w.update(rest, func(s *deviceState) { s.state, s.lastSeen = state, time.Now() })
}

func (w *watcher) update(device string, fn func(*deviceState)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	s, ok := w.states[device]
	if !ok {
		if len(w.states) >= maxTracked {
			return
		}
		s = &deviceState{}
		w.states[device] = s
	}
	fn(s)
}

func (w *watcher) handleBridge(sub string, payload []byte) {
	switch sub {
	case "state":
		if online, ok := ParseAvailability(payload); ok {
			w.mu.Lock()
			w.online = online
			w.mu.Unlock()
		}
	case "info":
		if info, err := ParseBridgeInfo(payload); err == nil {
			w.mu.Lock()
			w.info = info
			w.mu.Unlock()
		}
	case "devices":
		if len(payload) > maxDevicesBytes {
			log.Warn().Str("connection", w.conn.Name).Int("bytes", len(payload)).Msg("zigbee: bridge/devices too large; ignored")
			return
		}
		devices, err := ParseDevices(payload)
		if err != nil {
			log.Warn().Err(err).Str("connection", w.conn.Name).Msg("zigbee: bridge/devices ignored")
			return
		}
		w.mu.Lock()
		w.devices = devices
		w.mu.Unlock()
	}
}

func (w *watcher) network() Network {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := Network{Connection: w.conn.Name, BaseTopic: w.conn.BaseTopic,
		Bridge: BridgeStatus{Online: w.online, Info: w.info}, Devices: make([]DeviceStatus, 0, len(w.devices))}
	if w.sub != nil {
		n.Connected = w.sub.Conn().Connected()
	}
	for _, d := range w.devices {
		ds := DeviceStatus{Device: d}
		if s, ok := w.states[d.FriendlyName]; ok {
			ds.State = cloneState(s.state)
			ds.Online = s.online
			if !s.lastSeen.IsZero() {
				seen := s.lastSeen
				ds.LastSeen = &seen
			}
		}
		n.Devices = append(n.Devices, ds)
	}
	return n
}

// cloneState copies a state's top level (values are never changed in place).
func cloneState(s map[string]any) map[string]any {
	if s == nil {
		return nil
	}
	out := make(map[string]any, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}
