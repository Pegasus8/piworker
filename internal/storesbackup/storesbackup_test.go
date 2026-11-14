package storesbackup

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/vars"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const passphrase = "correct horse battery staple"

// fixture is an installation's stores: global variables, two flows (one
// with variables), two secrets and a connection with a password.
func fixture(t *testing.T) Sources {
	t.Helper()
	global := vars.NewStore()
	require.NoError(t, global.Set("threshold", 21.5))
	require.NoError(t, global.Set("room", map[string]any{"name": "kitchen"}))
	flowVars := map[string]*vars.Store{"f1": vars.NewStore(), "f2": vars.NewStore()}
	require.NoError(t, flowVars["f1"].Set("count", float64(3)))
	sec := secrets.NewStore()
	require.NoError(t, sec.Set("api_key", "zzSECRETzzAPIkey-value"))
	require.NoError(t, sec.Set("mqtt_pw", "zzSECRETzzMQTT"))
	conns := brokers.NewStore()
	require.NoError(t, conns.Set(brokers.Connection{Name: "home", Broker: "tcp://10.0.0.2:1883", Username: "pi", Password: "zzSECRETzzBROKER", BaseTopic: "z2m"}))
	require.NoError(t, conns.Set(brokers.Connection{Name: "lab", Broker: "tcp://10.0.0.3:1883", Password: "{{secret.mqtt_pw}}"}))
	return Sources{
		Globals: global, Secrets: sec, Connections: conns,
		Flows: []FlowStore{{ID: "f1", Name: "Heating", Vars: flowVars["f1"]}, {ID: "f2", Name: "Empty", Vars: flowVars["f2"]}},
	}
}

// Without a passphrase the export carries variables, the connections
// without passwords and the secret names only: no secret value, no
// password, in any form.
func TestExportWithoutPassphraseCarriesNoSecretValues(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	assert.Equal(t, Format, b.Format)
	assert.Equal(t, Version, b.Version)
	assert.Equal(t, map[string]any{"threshold": 21.5, "room": map[string]any{"name": "kitchen"}}, b.Variables)
	assert.Equal(t, []FlowVariables{{FlowID: "f1", FlowName: "Heating", Variables: map[string]any{"count": float64(3)}}}, b.FlowVariables)
	assert.Equal(t, []Connection{
		{Name: "home", Broker: "tcp://10.0.0.2:1883", Username: "pi", BaseTopic: "z2m", HasPassword: true},
		{Name: "lab", Broker: "tcp://10.0.0.3:1883", BaseTopic: brokers.DefaultBaseTopic, HasPassword: true},
	}, b.Connections)
	assert.Equal(t, []string{"api_key", "mqtt_pw"}, b.Secrets)
	assert.Nil(t, b.Sealed)
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "zzSECRETzz")
}

// With a passphrase the secret values and the whole connections (with
// their passwords) are sealed: none of them appears in the file, and only
// the passphrase opens them.
func TestExportWithPassphraseSealsSecretsAndConnections(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	require.NotNil(t, b.Sealed)
	assert.Empty(t, b.Connections, "connections travel sealed, so a password cannot be redirected to another broker")
	raw, err := json.Marshal(b)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "zzSECRETzz")
	assert.NotContains(t, string(raw), "10.0.0.2", "the sealed connections' brokers are not readable either")

	content, err := b.Sealed.Open(passphrase)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"api_key": "zzSECRETzzAPIkey-value", "mqtt_pw": "zzSECRETzzMQTT"}, content.Secrets)
	require.Len(t, content.Connections, 2)
	assert.Equal(t, "zzSECRETzzBROKER", content.Connections[0].Password)

	_, err = b.Sealed.Open("wrong passphrase, long enough")
	assert.ErrorIs(t, err, ErrWrongPassphrase)
	tampered := *b.Sealed
	tampered.Data = strings.Repeat("A", len(tampered.Data))
	_, err = tampered.Open(passphrase)
	assert.ErrorIs(t, err, ErrWrongPassphrase)
}

func TestExportRefusesAShortPassphrase(t *testing.T) {
	_, err := Export(fixture(t), "short")
	assert.ErrorIs(t, err, ErrWeakPassphrase)
}

// emptyDestination is a fresh installation with the flows "Heating" (a new
// ID, as an import of the flow gives it) and "Other".
func emptyDestination() (Sources, map[string]*vars.Store) {
	flowVars := map[string]*vars.Store{"n1": vars.NewStore(), "n2": vars.NewStore()}
	return Sources{
		Globals: vars.NewStore(), Secrets: secrets.NewStore(), Connections: brokers.NewStore(),
		Flows: []FlowStore{{ID: "n1", Name: "Heating", Vars: flowVars["n1"]}, {ID: "n2", Name: "Other", Vars: flowVars["n2"]}},
	}, flowVars
}

// Restoring a sealed export into a fresh installation recreates every
// store; flow variables follow their flow by ID, or by its unique name on
// another installation.
func TestRestoreRecreatesTheStores(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	dst, flowVars := emptyDestination()

	rep, err := Restore(b, passphrase, dst, Options{})
	require.NoError(t, err)
	v, _ := dst.Globals.Get("threshold")
	assert.Equal(t, 21.5, v)
	count, _ := flowVars["n1"].Get("count")
	assert.Equal(t, float64(3), count)
	s, _ := dst.Secrets.Get("api_key")
	assert.Equal(t, "zzSECRETzzAPIkey-value", s)
	c, ok := dst.Connections.Get("home")
	require.True(t, ok)
	assert.Equal(t, "zzSECRETzzBROKER", c.Password)
	assert.Equal(t, Report{
		Variables:            Counts{Added: 2},
		FlowVariables:        Counts{Added: 1},
		Secrets:              Counts{Added: 2},
		Connections:          Counts{Added: 2},
		ChangedConnections:   []string{"home", "lab"},
		UnmatchedFlows:       []FlowRef{},
		SecretsNotRestored:   []string{},
		PasswordsNotRestored: []string{},
	}, rep)
}

// By default what exists is kept (skipped); with Overwrite it is replaced.
// Without the passphrase only the plain part is restored and the secret
// names are reported as not restored.
func TestRestoreKeepsOrOverwritesAndNeedsThePassphraseForSecrets(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	dst, _ := emptyDestination()
	require.NoError(t, dst.Globals.Set("threshold", 99.0))
	require.NoError(t, dst.Secrets.Set("api_key", "local-value-kept"))

	rep, err := Restore(b, "", dst, Options{})
	require.NoError(t, err)
	v, _ := dst.Globals.Get("threshold")
	assert.Equal(t, 99.0, v, "kept")
	assert.Equal(t, Counts{Added: 1, Skipped: 1}, rep.Variables)
	assert.Equal(t, []string{"api_key", "mqtt_pw"}, rep.SecretsNotRestored)
	assert.Equal(t, Counts{}, rep.Connections, "sealed connections need the passphrase")
	s, _ := dst.Secrets.Get("api_key")
	assert.Equal(t, "local-value-kept", s)

	_, err = Restore(b, "wrong passphrase, long enough", dst, Options{})
	assert.ErrorIs(t, err, ErrWrongPassphrase)

	rep, err = Restore(b, passphrase, dst, Options{Overwrite: true})
	require.NoError(t, err)
	v, _ = dst.Globals.Get("threshold")
	assert.Equal(t, 21.5, v, "replaced")
	assert.Equal(t, Counts{Replaced: 2}, rep.Variables)
	assert.Equal(t, Counts{Added: 1, Replaced: 1}, rep.Secrets)
	s, _ = dst.Secrets.Get("api_key")
	assert.Equal(t, "zzSECRETzzAPIkey-value", s)
}

// Flow variables of a flow the installation does not have (by ID or by a
// unique name) are reported, not guessed.
func TestRestoreReportsUnmatchedFlows(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	b.FlowVariables = append(b.FlowVariables, FlowVariables{FlowID: "gone", FlowName: "Gone", Variables: map[string]any{"x": 1.0}})
	dst, _ := emptyDestination()
	dst.Flows = append(dst.Flows, FlowStore{ID: "n3", Name: "Heating", Vars: vars.NewStore()}) // "Heating" now ambiguous

	rep, err := Restore(b, "", dst, Options{})
	require.NoError(t, err)
	assert.Equal(t, []FlowRef{{ID: "f1", Name: "Heating"}, {ID: "gone", Name: "Gone"}}, rep.UnmatchedFlows)
	assert.Equal(t, Counts{}, rep.FlowVariables)
}

// A file that is not an export of this format, or has invalid names, is
// refused before anything is written.
func TestRestoreRefusesInvalidBundles(t *testing.T) {
	dst, _ := emptyDestination()
	_, err := Restore(&Bundle{Format: "other", Version: 1}, "", dst, Options{})
	assert.ErrorIs(t, err, ErrInvalidBundle)
	_, err = Restore(&Bundle{Format: Format, Version: 99}, "", dst, Options{})
	assert.ErrorIs(t, err, ErrInvalidBundle)
	_, err = Restore(&Bundle{Format: Format, Version: Version, Variables: map[string]any{"bad name": 1.0, "ok": 2.0}}, "", dst, Options{})
	assert.ErrorIs(t, err, ErrInvalidBundle)
	_, ok := dst.Globals.Get("ok")
	assert.False(t, ok, "nothing written")
}

// A connection from an export without a passphrase has no password: a new
// one is created without it (and reported), an existing one keeps its own.
func TestRestoreWithoutPasswordsKeepsTheExistingOnes(t *testing.T) {
	b, err := Export(fixture(t), "")
	require.NoError(t, err)
	dst, _ := emptyDestination()
	require.NoError(t, dst.Connections.Set(brokers.Connection{Name: "home", Broker: "tcp://old:1883", Password: "kept-password"}))

	rep, err := Restore(b, "", dst, Options{Overwrite: true})
	require.NoError(t, err)
	home, _ := dst.Connections.Get("home")
	assert.Equal(t, "tcp://10.0.0.2:1883", home.Broker, "replaced")
	assert.Equal(t, "kept-password", home.Password, "its own password kept")
	lab, _ := dst.Connections.Get("lab")
	assert.Empty(t, lab.Password)
	assert.Equal(t, []string{"lab"}, rep.PasswordsNotRestored)
	assert.Equal(t, Counts{Added: 1, Replaced: 1}, rep.Connections)
}

// Importing a connection identical to the one here keeps it (skipped) and
// does not list it as changed, so no flow restarts for nothing.
func TestRestoreSkipsIdenticalConnections(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	dst := fixture(t)
	rep, err := Restore(b, passphrase, dst, Options{Overwrite: true})
	require.NoError(t, err)
	assert.Equal(t, Counts{Skipped: 2}, rep.Connections)
	assert.Empty(t, rep.ChangedConnections)
}

// A file asking for more scrypt memory than PiWorker writes is refused
// before deriving anything.
func TestOpenRefusesHeavyScryptParameters(t *testing.T) {
	b, err := Export(fixture(t), passphrase)
	require.NoError(t, err)
	heavy := *b.Sealed
	heavy.N = 1 << 20
	_, err = heavy.Open(passphrase)
	assert.ErrorIs(t, err, ErrInvalidBundle)
}
