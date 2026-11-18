package brokers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memPersister struct {
	rows    map[string]Connection
	failing bool
}

func (m *memPersister) LoadConnections() (map[string]Connection, error) {
	out := map[string]Connection{}
	for k, v := range m.rows {
		out[k] = v
	}
	return out, nil
}

func (m *memPersister) SaveConnection(c Connection) error {
	if m.failing {
		return errors.New("disk full")
	}
	m.rows[c.Name] = c
	return nil
}

func (m *memPersister) DeleteConnection(name string) error {
	if m.failing {
		return errors.New("disk full")
	}
	delete(m.rows, name)
	return nil
}

func home() Connection {
	return Connection{Name: "home", Broker: "tcp://192.168.1.10:1883", Username: "pw", Password: "s3cret", BaseTopic: "zigbee2mqtt"}
}

func TestStoreSetGetListDelete(t *testing.T) {
	p := &memPersister{rows: map[string]Connection{}}
	s := NewStore()
	require.NoError(t, s.Attach(p))

	require.NoError(t, s.Set(home()))
	got, ok := s.Get("home")
	require.True(t, ok)
	assert.Equal(t, home(), got)
	assert.Equal(t, home(), p.rows["home"], "written through")
	assert.Equal(t, []string{"home"}, s.Names())

	require.NoError(t, s.Delete("home"))
	_, ok = s.Get("home")
	assert.False(t, ok)
	assert.Empty(t, p.rows)
}

func TestStoreAttachLoads(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Attach(&memPersister{rows: map[string]Connection{"home": home()}}))
	_, ok := s.Get("home")
	assert.True(t, ok)
}

// A failed write changes nothing in memory.
func TestStoreWriteFailureKeepsState(t *testing.T) {
	p := &memPersister{rows: map[string]Connection{}}
	s := NewStore()
	require.NoError(t, s.Attach(p))
	require.NoError(t, s.Set(home()))
	p.failing = true
	changed := home()
	changed.Broker = "tcp://other:1883"
	require.Error(t, s.Set(changed))
	got, _ := s.Get("home")
	assert.Equal(t, home().Broker, got.Broker)
	require.Error(t, s.Delete("home"))
	_, ok := s.Get("home")
	assert.True(t, ok)
}

func TestValidate(t *testing.T) {
	ok := home()
	require.NoError(t, ok.Validate())

	defaults := Connection{Name: "a", Broker: "mqtt://broker.local"}
	defaults.Normalize()
	require.NoError(t, defaults.Validate())
	assert.Error(t, Connection{Name: "a", Broker: "tcp://x:1883"}.Validate(), "no base topic before Normalize")

	for _, c := range []struct {
		name string
		mod  func(*Connection)
		want string
	}{
		{"bad name", func(c *Connection) { c.Name = "my home" }, "name"},
		{"empty name", func(c *Connection) { c.Name = "" }, "name"},
		{"long name", func(c *Connection) { c.Name = strings.Repeat("a", 65) }, "name"},
		{"no broker", func(c *Connection) { c.Broker = "" }, "broker"},
		{"bad scheme", func(c *Connection) { c.Broker = "http://x:1883" }, "scheme"},
		{"no host", func(c *Connection) { c.Broker = "tcp://:1883" }, "host"},
		{"path", func(c *Connection) { c.Broker = "tcp://x:1883/a" }, "path"},
		{"credentials in URL", func(c *Connection) { c.Broker = "tcp://u:s3cret@x:1883" }, "username and password"},
		{"wildcard base", func(c *Connection) { c.BaseTopic = "z2m/#" }, "base topic"},
		{"empty level", func(c *Connection) { c.BaseTopic = "z2m//x" }, "base topic"},
		{"leading slash", func(c *Connection) { c.BaseTopic = "/z2m" }, "base topic"},
	} {
		conn := home()
		c.mod(&conn)
		err := conn.Validate()
		require.Error(t, err, c.name)
		assert.Contains(t, err.Error(), c.want, c.name)
		assert.NotContains(t, err.Error(), "s3cret", "errors never quote the password")
	}
}

// Validate fills the default base topic.
func TestNormalize(t *testing.T) {
	c := Connection{Name: "a", Broker: " tcp://x:1883 ", BaseTopic: " "}
	c.Normalize()
	assert.Equal(t, "tcp://x:1883", c.Broker)
	assert.Equal(t, DefaultBaseTopic, c.BaseTopic)
}

func TestFromContext(t *testing.T) {
	assert.Same(t, DefaultStore, FromContext(context.Background()))
	//nolint:staticcheck // a nil context must not panic
	assert.Same(t, DefaultStore, FromContext(nil))

	require.NoError(t, DefaultStore.Set(home()))
	t.Cleanup(func() { _ = DefaultStore.Delete("home") })
	isolated := FromContext(WithStore(context.Background(), nil))
	assert.NotSame(t, DefaultStore, isolated)
	_, ok := isolated.Get("home")
	assert.False(t, ok, "a nil store is an empty one, never the real one")
}

// A connection's credentials report the secrets they reference, so the
// Secrets dialog can say a secret is used by a connection.
func TestConnectionSecretReferences(t *testing.T) {
	c := Connection{Username: "{{secret.u}}", Password: "{{ secret.p }}-{{secret.p}}"}
	assert.Equal(t, []SecretReference{{Name: "u", Setting: "username"}, {Name: "p", Setting: "password"}}, SecretReferences(c))
	assert.Empty(t, SecretReferences(Connection{Username: "plain"}))
}
