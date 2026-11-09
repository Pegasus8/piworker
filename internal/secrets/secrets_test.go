package secrets

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePersister records calls for write-through assertions.
type fakePersister struct {
	loaded  map[string]string
	saved   map[string]string
	deleted []string
}

func newFakePersister(initial map[string]string) *fakePersister {
	return &fakePersister{loaded: initial, saved: map[string]string{}}
}

func (f *fakePersister) LoadSecrets() (map[string]string, error) {
	out := map[string]string{}
	for k, v := range f.loaded {
		out[k] = v
	}
	return out, nil
}
func (f *fakePersister) SaveSecret(name, value string) error { f.saved[name] = value; return nil }
func (f *fakePersister) DeleteSecret(name string) error {
	f.deleted = append(f.deleted, name)
	return nil
}

func TestStoreSetGetDelete(t *testing.T) {
	s := NewStore()
	_, ok := s.Get("missing")
	assert.False(t, ok)

	require.NoError(t, s.Set("api_key", "s3cr3t"))
	v, ok := s.Get("api_key")
	assert.True(t, ok)
	assert.Equal(t, "s3cr3t", v)

	require.NoError(t, s.Delete("api_key"))
	_, ok = s.Get("api_key")
	assert.False(t, ok)
}

func TestStoreNamesAreSortedAndValuesHidden(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Set("zeta", "v1"))
	require.NoError(t, s.Set("alpha", "v2"))
	assert.Equal(t, []string{"alpha", "zeta"}, s.Names())
}

func TestStoreRejectsInvalidNames(t *testing.T) {
	s := NewStore()
	for _, bad := range []string{"", "has space", "dot.name", "weird!", "a/b"} {
		assert.Error(t, s.Set(bad, "x"), "name %q must be rejected", bad)
	}
	for _, good := range []string{"a", "API_KEY", "telegram-token", "x1_y2"} {
		assert.NoError(t, s.Set(good, "x"), "name %q must be accepted", good)
	}
}

func TestStoreWriteThroughPersister(t *testing.T) {
	p := newFakePersister(nil)
	s := NewStore()
	require.NoError(t, s.Attach(p))

	require.NoError(t, s.Set("token", "abc"))
	assert.Equal(t, "abc", p.saved["token"])

	require.NoError(t, s.Delete("token"))
	assert.Contains(t, p.deleted, "token")
}

func TestStoreAttachLoadsExisting(t *testing.T) {
	p := newFakePersister(map[string]string{"loaded_key": "from-db"})
	s := NewStore()
	require.NoError(t, s.Attach(p))

	v, ok := s.Get("loaded_key")
	assert.True(t, ok)
	assert.Equal(t, "from-db", v)
}

type unavailableSecrets struct{}

func (unavailableSecrets) LoadSecrets() (map[string]string, error) {
	return map[string]string{"TOKEN": "old"}, nil
}
func (unavailableSecrets) SaveSecret(string, string) error { return fmt.Errorf("storage unavailable") }
func (unavailableSecrets) DeleteSecret(string) error       { return fmt.Errorf("storage unavailable") }
func TestFailedPersistencePreservesSecret(t *testing.T) {
	s := NewStore()
	require.NoError(t, s.Attach(unavailableSecrets{}))
	require.Error(t, s.Set("TOKEN", "new"))
	v, _ := s.Get("TOKEN")
	require.Equal(t, "old", v)
	require.Error(t, s.Delete("TOKEN"))
	v, ok := s.Get("TOKEN")
	require.True(t, ok)
	require.Equal(t, "old", v)
}

func TestIsShortCountsRunesNotBytes(t *testing.T) {
	assert.Equal(t, 16, ShortValueRunes)
	cases := []struct {
		name  string
		value string
		short bool
	}{
		{"empty", "", true},
		{"pin", "1234", true},
		{"15 ASCII runes", strings.Repeat("a", 15), true},
		{"16 ASCII runes", strings.Repeat("a", 16), false},
		{"long", strings.Repeat("x", 64), false},
		// A multi-byte character counts once: 15 runes are short in 30 or
		// 60 bytes, 16 are not.
		{"15 two-byte runes", strings.Repeat("é", 15), true},
		{"15 four-byte runes", strings.Repeat("🔑", 15), true},
		{"16 three-byte runes", strings.Repeat("日", 16), false},
		{"15 mixed runes", "ñandú-日本-🔑🔑-abc", true},
		{"16 mixed runes", "ñandú-日本-🔑🔑-abcd", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.short, IsShort(tc.value), "a value of %d runes", utf8.RuneCountInString(tc.value))
		})
	}
}

func TestStoreListMarksShortValuesSortedByName(t *testing.T) {
	s := NewStore()
	assert.Empty(t, s.List())
	require.NoError(t, s.Set("pin", "1234"))
	require.NoError(t, s.Set("api_key", strings.Repeat("k", 16)))
	require.NoError(t, s.Set("accents", strings.Repeat("é", 15)))
	assert.Equal(t, []Entry{
		{Name: "accents", Short: true},
		{Name: "api_key", Short: false},
		{Name: "pin", Short: true},
	}, s.List())

	// Replacing a value updates its mark.
	require.NoError(t, s.Set("pin", "correct-horse-battery-staple"))
	assert.Equal(t, Entry{Name: "pin", Short: false}, s.List()[2])
}
