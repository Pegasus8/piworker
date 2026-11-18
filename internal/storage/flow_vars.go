package storage

import (
	"encoding/json"
	"fmt"
	"github.com/Pegasus8/piworker/internal/vars"
)

type flowVariables struct {
	store *SQLiteStore
	id    string
}

func (s *SQLiteStore) FlowVariables(id string) vars.Persister { return &flowVariables{s, id} }
func (p *flowVariables) LoadVars() (map[string]interface{}, error) {
	if p.store.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := p.store.db.Query(`SELECT key,value FROM flow_vars WHERE flow_id=?`, p.id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make(map[string]interface{})
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		var value interface{}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, rows.Err()
}
func (p *flowVariables) SaveVar(key string, value interface{}) error {
	if p.store.isClosed() {
		return ErrDatabaseClosed
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	result, err := p.store.db.Exec(`INSERT INTO flow_vars(flow_id,key,value) SELECT id,?,? FROM flows WHERE id=? ON CONFLICT(flow_id,key) DO UPDATE SET value=excluded.value`, key, string(raw), p.id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("flow not found")
	}
	return nil
}
func (p *flowVariables) DeleteVar(key string) error {
	if p.store.isClosed() {
		return ErrDatabaseClosed
	}
	_, err := p.store.db.Exec(`DELETE FROM flow_vars WHERE flow_id=? AND key=?`, p.id, key)
	return err
}
