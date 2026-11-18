package vars

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeVarPersister struct {
	loaded  map[string]interface{}
	saved   map[string]interface{}
	deleted []string
}

func newFakeVarPersister(initial map[string]interface{}) *fakeVarPersister {
	return &fakeVarPersister{loaded: initial, saved: map[string]interface{}{}}
}

func (f *fakeVarPersister) LoadVars() (map[string]interface{}, error) {
	out := map[string]interface{}{}
	for k, v := range f.loaded {
		out[k] = v
	}
	return out, nil
}
func (f *fakeVarPersister) SaveVar(key string, value interface{}) error {
	f.saved[key] = value
	return nil
}
func (f *fakeVarPersister) DeleteVar(key string) error {
	f.deleted = append(f.deleted, key)
	return nil
}

func TestStoreWriteThrough(t *testing.T) {
	p := newFakeVarPersister(nil)
	s := NewStore()
	require.NoError(t, s.Attach(p))

	s.Set("k", 42)
	assert.Equal(t, 42, p.saved["k"])

	s.Delete("k")
	assert.Contains(t, p.deleted, "k")
}

func TestStoreAttachLoadsExisting(t *testing.T) {
	p := newFakeVarPersister(map[string]interface{}{"loaded": "v"})
	s := NewStore()
	require.NoError(t, s.Attach(p))

	v, ok := s.Get("loaded")
	assert.True(t, ok)
	assert.Equal(t, "v", v)
}

func TestStoreWorksWithoutPersister(t *testing.T) {
	s := NewStore()
	s.Set("k", 1)
	v, ok := s.Get("k")
	assert.True(t, ok)
	assert.Equal(t, 1, v)
}

type unavailableVars struct{}

func (unavailableVars) LoadVars() (map[string]interface{}, error) {
	return map[string]interface{}{"kept": "old"}, nil
}
func (unavailableVars) SaveVar(string, interface{}) error { return fmt.Errorf("storage unavailable") }
func (unavailableVars) DeleteVar(string) error            { return fmt.Errorf("storage unavailable") }
func TestFailedPersistencePreservesVariable(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Attach(unavailableVars{}))
	s.Set("kept", "new")
	v, _ := s.Get("kept")
	require.Equal(t, "old", v)
	s.Delete("kept")
	v, ok := s.Get("kept")
	require.True(t, ok)
	require.Equal(t, "old", v)
}
func TestVariableValuesAreIsolated(t *testing.T) {
	s := NewStore()
	original := map[string]interface{}{"nested": map[string]interface{}{"v": 1}}
	s.Set("settings", original)
	original["nested"].(map[string]interface{})["v"] = 2
	snapshot := s.Snapshot()
	snapshot["settings"].(map[string]interface{})["nested"].(map[string]interface{})["v"] = 3
	got, _ := s.Get("settings")
	require.Equal(t, 1, got.(map[string]interface{})["nested"].(map[string]interface{})["v"])
}
