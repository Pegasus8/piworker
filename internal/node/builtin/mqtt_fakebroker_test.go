package builtin

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
)

// fakeBroker is a minimal in-process MQTT 3.1.1 broker for tests. It speaks
// just enough of the protocol (CONNECT, SUBSCRIBE, UNSUBSCRIBE, PUBLISH QoS
// 0/1, PINGREQ, DISCONNECT) to exercise the MQTT nodes without a real broker,
// and lets a test drop every connection to simulate a network flap. route
// delivers like Mosquitto's default (allow_duplicate_messages): one copy per
// matching subscription of each connection.
type fakeBroker struct {
	t  *testing.T
	ln net.Listener

	mu           sync.Mutex
	conns        map[net.Conn]bool
	filters      map[net.Conn][]string // each connection's subscriptions
	clientIDs    []string
	subscribes   []string
	unsubscribes []string
	publishes    []string
	disconnects  int
}

func newFakeBroker(t *testing.T) *fakeBroker {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	b := &fakeBroker{t: t, ln: ln, conns: map[net.Conn]bool{}, filters: map[net.Conn][]string{}}
	go b.serve()
	t.Cleanup(b.close)
	return b
}

func (b *fakeBroker) url() string { return "tcp://" + b.ln.Addr().String() }

func (b *fakeBroker) close() {
	_ = b.ln.Close()
	b.dropAll()
}

// dropAll closes every client connection without a DISCONNECT, the way a
// Wi-Fi drop or broker restart looks to the client.
func (b *fakeBroker) dropAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.conns {
		_ = c.Close()
		delete(b.conns, c)
		delete(b.filters, c)
	}
}

func (b *fakeBroker) snapshot() (clientIDs, subs, pubs []string, disconnects int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.clientIDs...), append([]string(nil), b.subscribes...),
		append([]string(nil), b.publishes...), b.disconnects
}

func (b *fakeBroker) connCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.conns)
}

// publishToAll sends a QoS 0 PUBLISH to every connected client.
func (b *fakeBroker) publishToAll(topic, payload string) {
	body := append(mqttString(topic), payload...)
	pkt := append([]byte{0x30}, mqttRemainingLength(len(body))...)
	pkt = append(pkt, body...)
	b.mu.Lock()
	defer b.mu.Unlock()
	for c := range b.conns {
		_, _ = c.Write(pkt)
	}
}

// route sends a QoS 0 PUBLISH of topic to each connection once per
// subscription of its that matches topic.
func (b *fakeBroker) route(topic, payload string) {
	body := append(mqttString(topic), payload...)
	pkt := append([]byte{0x30}, mqttRemainingLength(len(body))...)
	pkt = append(pkt, body...)
	b.mu.Lock()
	defer b.mu.Unlock()
	for c, filters := range b.filters {
		for _, f := range filters {
			if fakeTopicMatches(f, topic) {
				_, _ = c.Write(pkt)
			}
		}
	}
}

func (b *fakeBroker) unsubscribed() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.unsubscribes...)
}

// fakeTopicMatches reports whether an MQTT topic filter matches topic.
func fakeTopicMatches(filter, topic string) bool {
	f, t := strings.Split(filter, "/"), strings.Split(topic, "/")
	for i, level := range f {
		if level == "#" {
			return true
		}
		if i >= len(t) || (level != "+" && level != t[i]) {
			return false
		}
	}
	return len(f) == len(t)
}

func (b *fakeBroker) serve() {
	for {
		c, err := b.ln.Accept()
		if err != nil {
			return
		}
		b.mu.Lock()
		b.conns[c] = true
		b.mu.Unlock()
		go b.handle(c)
	}
}

func (b *fakeBroker) handle(c net.Conn) {
	defer func() {
		_ = c.Close()
		b.mu.Lock()
		delete(b.conns, c)
		delete(b.filters, c)
		b.mu.Unlock()
	}()
	r := bufio.NewReader(c)
	for {
		header, body, err := readMqttPacket(r)
		if err != nil {
			return
		}
		switch header >> 4 {
		case 1: // CONNECT
			// protocol name, level, flags, keepalive, then client id
			nameLen := int(binary.BigEndian.Uint16(body))
			p := 2 + nameLen + 4
			idLen := int(binary.BigEndian.Uint16(body[p:]))
			b.mu.Lock()
			b.clientIDs = append(b.clientIDs, string(body[p+2:p+2+idLen]))
			b.mu.Unlock()
			_, _ = c.Write([]byte{0x20, 0x02, 0x00, 0x00})
		case 3: // PUBLISH
			qos := (header >> 1) & 0x03
			tLen := int(binary.BigEndian.Uint16(body))
			topic := string(body[2 : 2+tLen])
			p := 2 + tLen
			var pid []byte
			if qos > 0 {
				pid = body[p : p+2]
				p += 2
			}
			b.mu.Lock()
			b.publishes = append(b.publishes, topic+"="+string(body[p:]))
			b.mu.Unlock()
			if qos == 1 {
				_, _ = c.Write([]byte{0x40, 0x02, pid[0], pid[1]})
			}
		case 8: // SUBSCRIBE
			pid := body[:2]
			p := 2
			var granted []byte
			for p < len(body) {
				tLen := int(binary.BigEndian.Uint16(body[p:]))
				topic := string(body[p+2 : p+2+tLen])
				granted = append(granted, body[p+2+tLen])
				p += 2 + tLen + 1
				b.mu.Lock()
				b.subscribes = append(b.subscribes, topic)
				if !slices.Contains(b.filters[c], topic) {
					b.filters[c] = append(b.filters[c], topic)
				}
				b.mu.Unlock()
			}
			resp := append([]byte{0x90, byte(2 + len(granted)), pid[0], pid[1]}, granted...)
			_, _ = c.Write(resp)
		case 10: // UNSUBSCRIBE
			for p := 2; p < len(body); {
				tLen := int(binary.BigEndian.Uint16(body[p:]))
				topic := string(body[p+2 : p+2+tLen])
				p += 2 + tLen
				b.mu.Lock()
				b.unsubscribes = append(b.unsubscribes, topic)
				b.filters[c] = slices.DeleteFunc(b.filters[c], func(f string) bool { return f == topic })
				b.mu.Unlock()
			}
			_, _ = c.Write([]byte{0xB0, 0x02, body[0], body[1]})
		case 12: // PINGREQ
			_, _ = c.Write([]byte{0xD0, 0x00})
		case 14: // DISCONNECT
			b.mu.Lock()
			b.disconnects++
			b.mu.Unlock()
			return
		}
	}
}

func readMqttPacket(r *bufio.Reader) (byte, []byte, error) {
	header, err := r.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length, mult := 0, 1
	for {
		d, err := r.ReadByte()
		if err != nil {
			return 0, nil, err
		}
		length += int(d&0x7F) * mult
		if d&0x80 == 0 {
			break
		}
		mult *= 128
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return 0, nil, err
	}
	return header, body, nil
}

func mqttString(s string) []byte {
	out := make([]byte, 2, 2+len(s))
	binary.BigEndian.PutUint16(out, uint16(len(s)))
	return append(out, s...)
}

func mqttRemainingLength(n int) []byte {
	var out []byte
	for {
		d := byte(n % 128)
		n /= 128
		if n > 0 {
			d |= 0x80
		}
		out = append(out, d)
		if n == 0 {
			return out
		}
	}
}
