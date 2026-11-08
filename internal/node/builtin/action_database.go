package builtin

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/Pegasus8/piworker/internal/node"
	"github.com/Pegasus8/piworker/internal/types"
	"github.com/expr-lang/expr/vm"

	// SQL drivers, registered for database/sql by side effect.
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"github.com/mattn/go-sqlite3"
)

// dbPool shares one *sql.DB (itself a connection pool) per driver+DSN between
// the nodes that use it, so a node doesn't reconnect per message. Each node
// holds at most one reference; the *sql.DB is closed when the last node using
// it is closed (its flow stopped).
var (
	dbPoolMu sync.Mutex
	dbPool   = map[string]*pooledDB{}
)

type pooledDB struct {
	db   *sql.DB
	refs int
}

func dbPoolKey(driverName, dsn string) string { return driverName + "\x00" + dsn }

// acquireDB returns the shared *sql.DB for driver+DSN, taking a reference.
func acquireDB(driverName, dsn string) (*sql.DB, error) {
	key := dbPoolKey(driverName, dsn)
	dbPoolMu.Lock()
	defer dbPoolMu.Unlock()
	if p, ok := dbPool[key]; ok {
		p.refs++
		return p.db, nil
	}
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, err
	}
	dbPool[key] = &pooledDB{db: db, refs: 1}
	return db, nil
}

// releaseDB drops a reference and closes the *sql.DB when none remain.
func releaseDB(driverName, dsn string) error {
	key := dbPoolKey(driverName, dsn)
	dbPoolMu.Lock()
	p, ok := dbPool[key]
	if !ok {
		dbPoolMu.Unlock()
		return nil
	}
	p.refs--
	if p.refs > 0 {
		dbPoolMu.Unlock()
		return nil
	}
	delete(dbPool, key)
	dbPoolMu.Unlock()
	return p.db.Close()
}

// sqlDriverName maps a friendly driver name to its registered database/sql name.
func sqlDriverName(driver string) (string, error) {
	switch strings.ToLower(driver) {
	case "sqlite", "sqlite3":
		return "sqlite3", nil
	case "postgres", "postgresql":
		return "postgres", nil
	case "mysql":
		return "mysql", nil
	default:
		return "", fmt.Errorf("unsupported driver %q (use sqlite, postgres or mysql)", driver)
	}
}

// DatabaseAction runs a SQL query (returning rows) or statement (returning
// affected-row counts) against SQLite, Postgres or MySQL.
type DatabaseAction struct {
	*node.BaseNode

	driverName string
	dsn        string
	query      string
	exec       bool
	params     *vm.Program

	dbMu sync.Mutex
	db   *sql.DB // shared pool reference, held from first use until Close
}

// The runtime closes nodes implementing node.Closer when a flow stops.
var _ node.Closer = (*DatabaseAction)(nil)

// database returns this node's reference to the shared pool, acquiring it on
// first use.
func (a *DatabaseAction) database() (*sql.DB, error) {
	a.dbMu.Lock()
	defer a.dbMu.Unlock()
	if a.db == nil {
		db, err := acquireDB(a.driverName, a.dsn)
		if err != nil {
			return nil, err
		}
		a.db = db
	}
	return a.db, nil
}

// Close releases this node's reference to the shared pool. It is idempotent.
func (a *DatabaseAction) Close() error {
	a.dbMu.Lock()
	defer a.dbMu.Unlock()
	if a.db == nil {
		return nil
	}
	a.db = nil
	return releaseDB(a.driverName, a.dsn)
}

// NewDatabaseAction creates a DatabaseAction from configuration.
func NewDatabaseAction(config map[string]interface{}) (node.Node, error) {
	base := node.NewBaseNode(config)

	driverName, err := sqlDriverName(base.GetConfigString("driver", "sqlite"))
	if err != nil {
		return nil, err
	}
	// DSN may hold credentials, so resolve {{secret.NAME}} (never templated from
	// payload).
	dsn := expandSecrets(base.GetConfigString("dsn", ""))
	if dsn == "" {
		return nil, fmt.Errorf("dsn is required")
	}
	query := base.GetConfigString("query", "")
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("query is required")
	}

	a := &DatabaseAction{
		BaseNode:   base,
		driverName: driverName,
		dsn:        dsn,
		query:      query,
		exec:       strings.ToLower(base.GetConfigString("mode", "query")) == "exec",
	}

	if p := base.GetConfigString("params", ""); strings.TrimSpace(p) != "" {
		prog, err := compileExpr(p)
		if err != nil {
			return nil, fmt.Errorf("invalid params expression: %w", err)
		}
		a.params = prog
	}
	return a, nil
}

// Process executes the query/statement and returns rows or affected counts.
func (a *DatabaseAction) Process(ctx context.Context, msg *types.Message) ([]*types.Message, error) {
	db, err := a.database()
	if err != nil {
		// sql.Open only validates the driver/DSN; it doesn't connect.
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	args, err := a.evalArgs(ctx, msg)
	if err != nil {
		return nil, err
	}

	out := msg.Clone()
	out.SourcePort = "output"

	if a.exec {
		res, err := db.ExecContext(ctx, a.query, args...)
		if err != nil {
			return nil, classifyDBError(fmt.Errorf("exec failed: %w", err), true)
		}
		affected, _ := res.RowsAffected()
		lastID, _ := res.LastInsertId()
		out.Payload = map[string]interface{}{"rowsAffected": affected, "lastInsertId": lastID}
		out.PayloadType = types.DataTypeObject
		return []*types.Message{out}, nil
	}

	rows, err := db.QueryContext(ctx, a.query, args...)
	if err != nil {
		return nil, classifyDBError(fmt.Errorf("query failed: %w", err), false)
	}
	defer rows.Close()

	scanned, err := scanRows(rows)
	if err != nil {
		return nil, fmt.Errorf("failed to read rows: %w", err)
	}
	out.Payload = scanned
	out.PayloadType = types.DataTypeArray
	out.SetMeta("rowCount", len(scanned))
	return []*types.Message{out}, nil
}

func (a *DatabaseAction) evalArgs(ctx context.Context, msg *types.Message) ([]interface{}, error) {
	if a.params == nil {
		return nil, nil
	}
	v, err := evalExpr(ctx, a.params, msg)
	if err != nil {
		return nil, fmt.Errorf("params evaluation failed: %w", err)
	}
	switch arr := v.(type) {
	case []interface{}:
		return arr, nil
	case nil:
		return nil, nil
	default:
		return []interface{}{arr}, nil
	}
}

// scanRows reads all rows into a slice of column→value maps, converting the
// []byte that some drivers return for text columns into strings.
func scanRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := make([]map[string]interface{}, 0)
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]interface{}, len(cols))
		for i, c := range cols {
			if b, ok := vals[i].([]byte); ok {
				row[c] = string(b)
			} else {
				row[c] = vals[i]
			}
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Ports returns the port definitions.
func (a *DatabaseAction) Ports() (inputs []types.Port, outputs []types.Port) {
	return []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		[]types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}}
}

// Validate checks the configuration.
func (a *DatabaseAction) Validate() error {
	if a.dsn == "" {
		return fmt.Errorf("dsn is required")
	}
	if strings.TrimSpace(a.query) == "" {
		return fmt.Errorf("query is required")
	}
	return nil
}

func databaseActionConfigSchema() node.ConfigSchema {
	return node.ConfigSchema{
		Properties: map[string]node.ConfigProperty{
			"driver": {
				Type:         "string",
				Title:        "Driver",
				Description:  "Database driver (sqlite3 and postgresql are accepted aliases)",
				Default:      "sqlite",
				Enum:         []string{"sqlite", "postgres", "mysql", "sqlite3", "postgresql"},
				AgentVisible: true,
			},
			"dsn": {
				Type:        "string",
				Title:       "DSN",
				Description: "Connection string (supports {{secret.NAME}} for credentials)",
				Sensitive:   true,
			},
			"mode": {
				Type:         "string",
				Title:        "Mode",
				Description:  "query returns rows; exec returns affected-row counts",
				Default:      "query",
				Enum:         []string{"query", "exec"},
				AgentVisible: true,
			},
			"query": {
				Type:        "string",
				Title:       "SQL",
				Description: "SQL statement with ? / $1 placeholders for parameters",
			},
			"params": {
				Type:        "string",
				Title:       "Parameters",
				Description: "Expression returning an array of bind parameters, e.g. [payload.id, payload.name]",
			},
		},
		Required: []string{"driver", "dsn", "query"},
	}
}

// GetConfigSchema returns the configuration schema for the UI.
func (a *DatabaseAction) GetConfigSchema() node.ConfigSchema {
	return databaseActionConfigSchema()
}

// DatabaseActionInfo returns the node type info for registration.
func DatabaseActionInfo() node.NodeTypeInfo {
	schema := databaseActionConfigSchema()
	return node.NodeTypeInfo{
		Type:        "action-database",
		Name:        "Database",
		Description: "Run a SQL query or statement against SQLite, Postgres or MySQL",
		Documentation: `## Database

Runs SQL against SQLite, Postgres or MySQL. Parameters are bound (never string-
interpolated) to prevent SQL injection.

## Configuration

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| driver | enum | sqlite | ` + "`sqlite`" + `, ` + "`postgres`" + ` or ` + "`mysql`" + ` |
| dsn | string | - | Connection string; use ` + "`{{secret.NAME}}`" + ` for credentials |
| mode | enum | query | ` + "`query`" + ` (rows) or ` + "`exec`" + ` (affected counts) |
| query | string | - | SQL with ` + "`?`" + `/` + "`$1`" + ` placeholders |
| params | string | - | Expression returning the bind-parameter array |

## Output

- ` + "`query`" + `: an array of row objects (column → value).
- ` + "`exec`" + `: ` + "`{ rowsAffected, lastInsertId }`" + `.

## Errors and retries

SQL errors (syntax, constraint violations, ...) fail the message without
retry. Only failures that happen before the statement runs (connection
refused, broken pooled connection, SQLite busy/locked) are retried; in
` + "`query`" + ` mode other network errors are retried too. An ` + "`exec`" + `
whose result was lost in transit is not retried, so an INSERT is never
duplicated.

## Example

` + "```" + `
query:  SELECT * FROM readings WHERE sensor = ? AND value > ?
params: [payload.sensor, payload.threshold]
` + "```" + `
`,
		Category: types.NodeCategoryOutput,
		Risk:     node.RiskPrivileged,
		Inputs:   []types.Port{{ID: "input", Name: "Input", DataType: types.DataTypeAny, Required: true}},
		Outputs:  []types.Port{{ID: "output", Name: "Output", DataType: types.DataTypeAny, Multiple: true}},
		Config:   &schema,
		Icon:     "database",
	}
}

func init() {
	info := DatabaseActionInfo()
	if err := node.Register(info, NewDatabaseAction); err != nil {
		panic("failed to register database action: " + err.Error())
	}
}

// classifyDBError decides whether a database error may be retried. Errors are
// permanent by default: syntax errors and constraint violations can't succeed
// on retry, and re-running a statement whose outcome is unknown can duplicate
// an INSERT. Only failures that happen before the statement runs are marked
// transient:
//   - driver.ErrBadConn (the driver guarantees nothing was sent);
//   - a failed dial (connection refused, unreachable, DNS);
//   - SQLite BUSY/LOCKED (the statement was not executed);
//   - for read-only queries (exec=false), any network error.
func classifyDBError(err error, exec bool) error {
	if errors.Is(err, driver.ErrBadConn) {
		return node.Transient(err)
	}
	var sqliteErr sqlite3.Error
	if errors.As(err, &sqliteErr) &&
		(sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked) {
		return node.Transient(err)
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return node.Transient(err)
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return node.Transient(err)
	}
	if !exec {
		var netErr net.Error
		if errors.As(err, &netErr) {
			return node.Transient(err)
		}
	}
	return err
}
