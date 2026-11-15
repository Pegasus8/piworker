// Package storesbackup exports and restores the installation's stores that a
// flow export does not carry (ADR-0031): the global variables, each flow's
// variables, the named broker connections, the LLM connections (ADR-0038)
// and the secrets. A flow export names secrets and connections but holds
// none of their values; this file is how they move to another installation
// or survive a reinstall.
//
// The file is JSON. Variables, connection settings without passwords or API
// keys and secret names are readable. Secret values and connections with
// their passwords or API keys travel only sealed with a passphrase (scrypt,
// then AES-256-GCM): the whole connection is sealed, not only its
// credential, so someone who edits the file cannot point a password at
// another broker or an API key at another endpoint. PiWorker does not
// encrypt its own database (ADR-0030); this is the one place it writes
// secret values to a file, so the file is encrypted.
package storesbackup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/Pegasus8/piworker/internal/brokers"
	"github.com/Pegasus8/piworker/internal/llm"
	"github.com/Pegasus8/piworker/internal/secrets"
	"github.com/Pegasus8/piworker/internal/vars"
	"golang.org/x/crypto/scrypt"
)

// Format and Version identify the file. The LLM connections were added to
// version 1 as new members: a PiWorker that predates them ignores them, and
// a file without them restores none.
const (
	Format  = "piworker-stores"
	Version = 1
)

// MinPassphraseRunes is the shortest passphrase that seals secrets: the
// file may be copied anywhere, so its passphrase must resist offline
// guessing (scrypt slows each guess, length does the rest).
const MinPassphraseRunes = 12

// scrypt parameters for new files (about 32 MiB and well under a second on
// a Raspberry Pi 3) and the largest a file may ask for when opened, so a
// crafted file cannot make the device spend minutes or gigabytes.
const (
	scryptN, scryptR, scryptP = 1 << 15, 8, 1
	maxScryptN, maxScryptR    = 1 << 16, 8 // 64 MiB at most: a Pi 3 has 1 GiB
	maxScryptP                = 1
	keyLen                    = 32
	saltLen                   = 16
)

// sealAAD binds the sealed data to this format and version.
var sealAAD = []byte(Format + "/1")

var (
	// ErrWrongPassphrase: the passphrase does not open the sealed part, or
	// the sealed part was changed.
	ErrWrongPassphrase = errors.New("wrong passphrase, or the file was changed")
	// ErrWeakPassphrase: a passphrase shorter than MinPassphraseRunes.
	ErrWeakPassphrase = fmt.Errorf("the passphrase must have at least %d characters", MinPassphraseRunes)
	// ErrInvalidBundle: not a stores export, or one with invalid content.
	ErrInvalidBundle = errors.New("not a valid PiWorker stores export")
)

// Bundle is the exported file.
type Bundle struct {
	Format    string    `json:"format"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	// Variables are the global variables.
	Variables map[string]any `json:"variables"`
	// FlowVariables are the variables of each flow that has any.
	FlowVariables []FlowVariables `json:"flowVariables"`
	// Connections are the broker connections without passwords; empty when
	// they are sealed.
	Connections []Connection `json:"connections"`
	// LLMConnections are the LLM connections without API keys; empty when
	// they are sealed.
	LLMConnections []LLMConnection `json:"llmConnections"`
	// Secrets are the names of the secrets (their values only sealed).
	Secrets []string `json:"secrets"`
	// Sealed holds the secret values, the broker connections with their
	// passwords and the LLM connections with their API keys, when the export
	// had a passphrase.
	Sealed *Sealed `json:"sealed,omitempty"`
}

// FlowVariables are one flow's variables, with the flow's ID and name at
// export time (a restore matches the ID, or else a unique name).
type FlowVariables struct {
	FlowID    string         `json:"flowId"`
	FlowName  string         `json:"flowName"`
	Variables map[string]any `json:"variables"`
}

// Connection is a broker connection in the file. Password is set only
// inside the sealed part.
type Connection struct {
	Name        string `json:"name"`
	Broker      string `json:"broker"`
	Username    string `json:"username"`
	Password    string `json:"password,omitempty"`
	BaseTopic   string `json:"baseTopic"`
	HasPassword bool   `json:"hasPassword"`
}

// LLMConnection is an LLM connection in the file. APIKey (as stored: plain
// or {{secret.NAME}} references) is set only inside the sealed part.
type LLMConnection struct {
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	BaseURL         string `json:"baseUrl,omitempty"`
	APIKey          string `json:"apiKey,omitempty"`
	Model           string `json:"model,omitempty"`
	DailyTokenLimit int64  `json:"dailyTokenLimit"`
	HasAPIKey       bool   `json:"hasApiKey"`
}

// connection is c as the LLM store holds it.
func (c LLMConnection) connection() llm.Connection {
	return llm.Connection{Name: c.Name, Provider: llm.Provider(c.Provider), BaseURL: c.BaseURL, APIKey: c.APIKey,
		Model: c.Model, DailyTokenLimit: c.DailyTokenLimit}
}

// SealedContent is what the sealed part holds.
type SealedContent struct {
	Secrets        map[string]string `json:"secrets"`
	Connections    []Connection      `json:"connections"`
	LLMConnections []LLMConnection   `json:"llmConnections"`
}

// Sealed is SealedContent encrypted with a key derived from a passphrase.
type Sealed struct {
	KDF    string `json:"kdf"` // "scrypt"
	N      int    `json:"n"`
	R      int    `json:"r"`
	P      int    `json:"p"`
	Salt   string `json:"salt"`   // base64
	Cipher string `json:"cipher"` // "aes-256-gcm"
	Nonce  string `json:"nonce"`  // base64
	Data   string `json:"data"`   // base64 ciphertext with its tag
}

// FlowStore is one flow's variable store, with the flow's ID and name.
type FlowStore struct {
	ID   string
	Name string
	Vars *vars.Store
}

// Sources are the stores an export reads and a restore writes.
// LLMConnections may be nil: an export then carries none, and a restore
// skips the file's.
type Sources struct {
	Globals        *vars.Store
	Flows          []FlowStore
	Secrets        *secrets.Store
	Connections    *brokers.Store
	LLMConnections *llm.Store
}

// Export builds the file from src. With a passphrase (at least
// MinPassphraseRunes characters) the secret values and the connections with
// their passwords and API keys are sealed; without one the file carries
// none of them: only the secret names and the connections without
// passwords or API keys.
func Export(src Sources, passphrase string) (*Bundle, error) {
	if passphrase != "" {
		if err := CheckPassphrase(passphrase); err != nil {
			return nil, err
		}
	}
	b := &Bundle{
		Format: Format, Version: Version, CreatedAt: time.Now().UTC(),
		Variables: map[string]any{}, FlowVariables: []FlowVariables{}, Connections: []Connection{},
		LLMConnections: []LLMConnection{}, Secrets: []string{},
	}
	if src.Globals != nil {
		b.Variables = src.Globals.Snapshot()
	}
	for _, f := range src.Flows {
		if f.Vars == nil {
			continue
		}
		if snap := f.Vars.Snapshot(); len(snap) > 0 {
			b.FlowVariables = append(b.FlowVariables, FlowVariables{FlowID: f.ID, FlowName: f.Name, Variables: snap})
		}
	}
	conns := []Connection{}
	if src.Connections != nil {
		for _, c := range src.Connections.List() {
			conns = append(conns, Connection{Name: c.Name, Broker: c.Broker, Username: c.Username, BaseTopic: c.BaseTopic,
				Password: c.Password, HasPassword: c.Password != ""})
		}
	}
	llmConns := []LLMConnection{}
	if src.LLMConnections != nil {
		for _, c := range src.LLMConnections.List() {
			llmConns = append(llmConns, LLMConnection{Name: c.Name, Provider: string(c.Provider), BaseURL: c.BaseURL,
				APIKey: c.APIKey, Model: c.Model, DailyTokenLimit: c.DailyTokenLimit, HasAPIKey: c.APIKey != ""})
		}
	}
	content := SealedContent{Secrets: map[string]string{}, Connections: conns, LLMConnections: llmConns}
	if src.Secrets != nil {
		for _, name := range src.Secrets.Names() {
			b.Secrets = append(b.Secrets, name)
			if v, ok := src.Secrets.Get(name); ok {
				content.Secrets[name] = v
			}
		}
	}
	if passphrase == "" {
		for _, c := range conns {
			c.Password = ""
			b.Connections = append(b.Connections, c)
		}
		for _, c := range llmConns {
			c.APIKey = "" // even a {{secret.NAME}} reference: the key is the admin's
			b.LLMConnections = append(b.LLMConnections, c)
		}
		return b, nil
	}
	sealed, err := seal(content, passphrase)
	if err != nil {
		return nil, err
	}
	b.Sealed = sealed
	return b, nil
}

// CheckPassphrase reports ErrWeakPassphrase for a passphrase too short to
// seal secrets (MinPassphraseRunes characters, counted as runes).
func CheckPassphrase(passphrase string) error {
	if utf8.RuneCountInString(passphrase) < MinPassphraseRunes {
		return ErrWeakPassphrase
	}
	return nil
}

// seal encrypts content with a key derived from passphrase.
func seal(content SealedContent, passphrase string) (*Sealed, error) {
	plain, err := json.Marshal(content)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	key, err := scrypt.Key([]byte(passphrase), salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return nil, err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	enc := base64.StdEncoding
	return &Sealed{
		KDF: "scrypt", N: scryptN, R: scryptR, P: scryptP, Salt: enc.EncodeToString(salt),
		Cipher: "aes-256-gcm", Nonce: enc.EncodeToString(nonce), Data: enc.EncodeToString(gcm.Seal(nil, nonce, plain, sealAAD)),
	}, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Open decrypts the sealed part with passphrase. A wrong passphrase and a
// changed file are the same error (ErrWrongPassphrase); parameters outside
// what PiWorker writes are ErrInvalidBundle.
func (s *Sealed) Open(passphrase string) (*SealedContent, error) {
	if s.KDF != "scrypt" || s.Cipher != "aes-256-gcm" ||
		s.N < 2 || s.N > maxScryptN || s.N&(s.N-1) != 0 || s.R < 1 || s.R > maxScryptR || s.P < 1 || s.P > maxScryptP {
		return nil, fmt.Errorf("%w: unsupported encryption parameters", ErrInvalidBundle)
	}
	enc := base64.StdEncoding
	salt, err1 := enc.DecodeString(s.Salt)
	nonce, err2 := enc.DecodeString(s.Nonce)
	data, err3 := enc.DecodeString(s.Data)
	if err := errors.Join(err1, err2, err3); err != nil {
		return nil, ErrWrongPassphrase
	}
	key, err := scrypt.Key([]byte(passphrase), salt, s.N, s.R, s.P, keyLen)
	if err != nil {
		return nil, fmt.Errorf("%w: unsupported encryption parameters", ErrInvalidBundle)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, ErrWrongPassphrase
	}
	plain, err := gcm.Open(nil, nonce, data, sealAAD)
	if err != nil {
		return nil, ErrWrongPassphrase
	}
	var content SealedContent
	if err := json.Unmarshal(plain, &content); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidBundle, err)
	}
	return &content, nil
}

// Options control a restore.
type Options struct {
	// Overwrite replaces values that exist; otherwise they are kept and
	// counted as skipped.
	Overwrite bool
}

// Counts is what a restore did to one kind of entry.
type Counts struct {
	Added    int `json:"added"`
	Replaced int `json:"replaced"`
	Skipped  int `json:"skipped"`
}

// FlowRef names a flow of the file.
type FlowRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Report is what a restore did. Lists are never nil.
type Report struct {
	Variables     Counts `json:"variables"`
	FlowVariables Counts `json:"flowVariables"`
	Secrets       Counts `json:"secrets"`
	Connections   Counts `json:"connections"`
	// LLMConnections counts the LLM connections. Changing one restarts no
	// flow (the nodes resolve it on every call), so they are never in
	// ChangedConnections.
	LLMConnections Counts `json:"llmConnections"`
	// ChangedConnections were added or replaced: the caller restarts the
	// running flows that use them (ADR-0029).
	ChangedConnections []string `json:"changedConnections"`
	// UnmatchedFlows are the flows of the file whose variables found no
	// flow here (by ID, or by a unique name).
	UnmatchedFlows []FlowRef `json:"unmatchedFlows"`
	// SecretsNotRestored are the secret names of the file whose values
	// were not restored: the file has none, or no passphrase was given.
	SecretsNotRestored []string `json:"secretsNotRestored"`
	// PasswordsNotRestored are the connections created without the
	// password they had (the file did not carry it).
	PasswordsNotRestored []string `json:"passwordsNotRestored"`
	// APIKeysNotRestored are the LLM connections whose API key the file did
	// not carry and that have none here for the same endpoint: an
	// OpenAI-compatible one was restored without a key; an Anthropic one,
	// which cannot work without a key, was not (counted as skipped).
	APIKeysNotRestored []string `json:"apiKeysNotRestored"`
}

// Restore writes b into dst: secrets first (connection credentials may
// reference them), then broker connections, LLM connections, global
// variables and flow variables.
// The sealed part is opened with passphrase; without one, only the readable
// part is restored and the secret names are reported. Everything is
// checked before anything is written; a write error stops the restore
// where it happened (what was written stays).
func Restore(b *Bundle, passphrase string, dst Sources, opts Options) (Report, error) {
	rep := Report{ChangedConnections: []string{}, UnmatchedFlows: []FlowRef{}, SecretsNotRestored: []string{},
		PasswordsNotRestored: []string{}, APIKeysNotRestored: []string{}}
	if err := b.validate(); err != nil {
		return rep, err
	}
	conns, llmConns := b.Connections, b.LLMConnections
	var secretValues map[string]string
	if b.Sealed != nil && passphrase != "" {
		content, err := b.Sealed.Open(passphrase)
		if err != nil {
			return rep, err
		}
		if err := content.validate(); err != nil {
			return rep, err
		}
		secretValues, conns, llmConns = content.Secrets, content.Connections, content.LLMConnections
	} else {
		rep.SecretsNotRestored = append(rep.SecretsNotRestored, b.Secrets...)
		sort.Strings(rep.SecretsNotRestored)
	}

	// Secrets.
	for _, name := range slices.Sorted(maps.Keys(secretValues)) {
		_, exists := dst.Secrets.Get(name)
		if exists && !opts.Overwrite {
			rep.Secrets.Skipped++
			continue
		}
		if err := dst.Secrets.Set(name, secretValues[name]); err != nil {
			return rep, err
		}
		count(&rep.Secrets, exists)
	}

	// Connections.
	for _, c := range conns {
		old, exists := dst.Connections.Get(c.Name)
		if exists && !opts.Overwrite {
			rep.Connections.Skipped++
			continue
		}
		conn := brokers.Connection{Name: c.Name, Broker: c.Broker, Username: c.Username, Password: c.Password, BaseTopic: c.BaseTopic}
		if c.HasPassword && c.Password == "" {
			if exists {
				conn.Password = old.Password // the file does not carry it: keep this installation's
			} else {
				rep.PasswordsNotRestored = append(rep.PasswordsNotRestored, c.Name)
			}
		}
		conn.Normalize()
		if exists && conn == old {
			rep.Connections.Skipped++ // identical: nothing to restart
			continue
		}
		if err := dst.Connections.Set(conn); err != nil {
			return rep, err
		}
		count(&rep.Connections, exists)
		rep.ChangedConnections = append(rep.ChangedConnections, c.Name)
	}
	sort.Strings(rep.ChangedConnections)

	// LLM connections.
	if err := restoreLLMConnections(llmConns, dst.LLMConnections, opts, &rep); err != nil {
		return rep, err
	}

	// Global variables.
	if err := restoreVars(b.Variables, dst.Globals, opts, &rep.Variables); err != nil {
		return rep, err
	}

	// Flow variables: by ID, or else by a name only one flow here has.
	byID := map[string]FlowStore{}
	byName := map[string][]FlowStore{}
	for _, f := range dst.Flows {
		byID[f.ID] = f
		byName[f.Name] = append(byName[f.Name], f)
	}
	for _, fv := range b.FlowVariables {
		target, ok := byID[fv.FlowID]
		if !ok && len(byName[fv.FlowName]) == 1 {
			target, ok = byName[fv.FlowName][0], true
		}
		if !ok || target.Vars == nil {
			rep.UnmatchedFlows = append(rep.UnmatchedFlows, FlowRef{ID: fv.FlowID, Name: fv.FlowName})
			continue
		}
		if err := restoreVars(fv.Variables, target.Vars, opts, &rep.FlowVariables); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

func count(c *Counts, existed bool) {
	if existed {
		c.Replaced++
	} else {
		c.Added++
	}
}

// restoreLLMConnections writes conns into store (nil: each is skipped).
// A connection without its key in the file keeps the one it has here while
// the file leaves it at the same provider and endpoint: the readable part
// is not authenticated, and an edited one must not send this
// installation's key elsewhere. Otherwise it is restored without a key,
// unless it cannot work without one (Anthropic: then it is left as it is,
// or not created); both are reported in APIKeysNotRestored. Nothing
// restarts.
func restoreLLMConnections(conns []LLMConnection, store *llm.Store, opts Options, rep *Report) error {
	defer sort.Strings(rep.APIKeysNotRestored)
	for _, c := range conns {
		if store == nil {
			rep.LLMConnections.Skipped++
			continue
		}
		old, exists := store.Get(c.Name)
		if exists && !opts.Overwrite {
			rep.LLMConnections.Skipped++
			continue
		}
		conn := c.connection()
		conn.Normalize()
		if c.HasAPIKey && conn.APIKey == "" {
			sameEndpoint := exists && old.Provider == conn.Provider && old.Endpoint() == conn.Endpoint()
			switch {
			case sameEndpoint:
				conn.APIKey = old.APIKey // the file does not carry it: keep this installation's
			case conn.Provider == llm.ProviderAnthropic:
				rep.APIKeysNotRestored = append(rep.APIKeysNotRestored, c.Name)
				rep.LLMConnections.Skipped++
				continue
			default:
				rep.APIKeysNotRestored = append(rep.APIKeysNotRestored, c.Name)
			}
		}
		if exists && conn == old {
			rep.LLMConnections.Skipped++ // identical
			continue
		}
		if err := store.Set(conn); err != nil {
			return err
		}
		count(&rep.LLMConnections, exists)
	}
	return nil
}

func restoreVars(values map[string]any, store *vars.Store, opts Options, c *Counts) error {
	for _, k := range slices.Sorted(maps.Keys(values)) {
		_, exists := store.Get(k)
		if exists && !opts.Overwrite {
			c.Skipped++
			continue
		}
		if err := store.Set(k, values[k]); err != nil {
			return err
		}
		count(c, exists)
	}
	return nil
}

// validate checks the readable part.
func (b *Bundle) validate() error {
	if b == nil || b.Format != Format {
		return fmt.Errorf("%w: unknown format", ErrInvalidBundle)
	}
	if b.Version != Version {
		return fmt.Errorf("%w: version %d is not supported (this PiWorker reads version %d)", ErrInvalidBundle, b.Version, Version)
	}
	for k := range b.Variables {
		if !vars.ValidName(k) {
			return fmt.Errorf("%w: invalid variable name %q", ErrInvalidBundle, k)
		}
	}
	for _, fv := range b.FlowVariables {
		for k := range fv.Variables {
			if !vars.ValidName(k) {
				return fmt.Errorf("%w: invalid variable name %q in flow %q", ErrInvalidBundle, k, fv.FlowName)
			}
		}
	}
	for _, name := range b.Secrets {
		if !secrets.ValidName(name) {
			return fmt.Errorf("%w: invalid secret name %q", ErrInvalidBundle, name)
		}
	}
	if err := validateConnections(b.Connections); err != nil {
		return err
	}
	return validateLLMConnections(b.LLMConnections)
}

// validate checks the sealed part.
func (c *SealedContent) validate() error {
	for name := range c.Secrets {
		if !secrets.ValidName(name) {
			return fmt.Errorf("%w: invalid secret name %q", ErrInvalidBundle, name)
		}
	}
	if err := validateConnections(c.Connections); err != nil {
		return err
	}
	return validateLLMConnections(c.LLMConnections)
}

// validateConnections checks each connection as the store would, so a
// restore never stops halfway on a bad one. Errors never quote passwords.
func validateConnections(conns []Connection) error {
	for _, c := range conns {
		conn := brokers.Connection{Name: c.Name, Broker: c.Broker, Username: c.Username, BaseTopic: c.BaseTopic}
		conn.Normalize()
		if err := conn.Validate(); err != nil {
			return fmt.Errorf("%w: connection %q: %v", ErrInvalidBundle, c.Name, err)
		}
	}
	return nil
}

// validateLLMConnections checks each LLM connection as the store would
// (llm.Connection.Validate), so a restore never stops halfway on a bad one.
// A key the file says exists but does not carry counts as present: the
// restore keeps this installation's or reports it. Errors never quote a key
// or a base URL (llm.Connection.Validate quotes neither).
func validateLLMConnections(conns []LLMConnection) error {
	for _, c := range conns {
		conn := c.connection()
		if c.HasAPIKey && conn.APIKey == "" {
			conn.APIKey = "present"
		}
		conn.Normalize()
		if err := conn.Validate(); err != nil {
			return fmt.Errorf("%w: LLM connection %q: %v", ErrInvalidBundle, c.Name, err)
		}
	}
	return nil
}
