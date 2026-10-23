package storage

import (
	"fmt"
	"time"
)

// FlowRun is one run of a flow — the wave of node executions that follow a
// single trigger firing, identified by the runtime's correlation ID.
type FlowRun struct {
	ID         string     `json:"id"`
	FlowID     string     `json:"flowId"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Status     string     `json:"status"` // running|success|error
	NodeCount  int        `json:"nodeCount"`
	ErrorCount int        `json:"errorCount"`
}

// NodeEventRecord is one persisted node-execution observation belonging to a run.
type NodeEventRecord struct {
	ID         int64     `json:"id"`
	RunID      string    `json:"runId"`
	FlowID     string    `json:"flowId"`
	NodeID     string    `json:"nodeId"`
	NodeType   string    `json:"nodeType"`
	Phase      string    `json:"phase"` // running|success|error
	MsgID      string    `json:"msgId,omitempty"`
	DurationMs float64   `json:"durationMs,omitempty"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// UpsertFlowRun records the start of a run. It is idempotent: repeated calls for
// the same run ID (one per node event in the run) neither duplicate the row nor
// move the recorded start time.
func (s *SQLiteStore) UpsertFlowRun(runID, flowID string, startedAt time.Time) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	_, err := s.db.Exec(`
		INSERT INTO flow_runs (id, flow_id, started_at, status)
		VALUES (?, ?, ?, 'running')
		ON CONFLICT(id) DO NOTHING
	`, runID, flowID, startedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert flow run: %w", err)
	}
	return nil
}

// FinishFlowRun marks a run complete with its final status and aggregate counts.
func (s *SQLiteStore) FinishFlowRun(runID, status string, finishedAt time.Time, nodeCount, errorCount int) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	_, err := s.db.Exec(`
		UPDATE flow_runs
		SET status = ?, finished_at = ?, node_count = ?, error_count = ?
		WHERE id = ?
	`, status, finishedAt, nodeCount, errorCount, runID)
	if err != nil {
		return fmt.Errorf("failed to finish flow run: %w", err)
	}
	return nil
}

// InsertNodeEvents writes a batch of node events in a single transaction. This is
// the only history write path on the hot side and is called exclusively by the
// recorder's single writer goroutine, so it never contends with itself under the
// store's single-connection pool. An empty batch is a no-op.
func (s *SQLiteStore) InsertNodeEvents(batch []NodeEventRecord) error {
	if s.isClosed() {
		return ErrDatabaseClosed
	}
	if len(batch) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin node-events tx: %w", err)
	}
	stmt, err := tx.Prepare(`
		INSERT INTO node_events
			(run_id, flow_id, node_id, node_type, phase, msg_id, duration_ms, error, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("failed to prepare node-events insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range batch {
		if _, err := stmt.Exec(e.RunID, e.FlowID, e.NodeID, e.NodeType, e.Phase, e.MsgID, e.DurationMs, e.Error, e.CreatedAt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("failed to insert node event: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit node events: %w", err)
	}
	return nil
}

// ListFlowRuns returns a flow's runs, newest first, with limit/offset pagination.
func (s *SQLiteStore) ListFlowRuns(flowID string, limit, offset int) ([]FlowRun, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`
		SELECT id, flow_id, started_at, finished_at, status, node_count, error_count
		FROM flow_runs
		WHERE flow_id = ?
		ORDER BY started_at DESC
		LIMIT ? OFFSET ?
	`, flowID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list flow runs: %w", err)
	}
	defer rows.Close()

	var runs []FlowRun
	for rows.Next() {
		var r FlowRun
		if err := rows.Scan(&r.ID, &r.FlowID, &r.StartedAt, &r.FinishedAt, &r.Status, &r.NodeCount, &r.ErrorCount); err != nil {
			return nil, fmt.Errorf("failed to scan flow run: %w", err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating flow runs: %w", err)
	}
	return runs, nil
}

// ListNodeEvents returns all node events for a run in execution order.
func (s *SQLiteStore) ListNodeEvents(runID string) ([]NodeEventRecord, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	rows, err := s.db.Query(`
		SELECT id, run_id, flow_id, node_id, node_type, phase, msg_id, duration_ms, error, created_at
		FROM node_events
		WHERE run_id = ?
		ORDER BY id ASC
	`, runID)
	if err != nil {
		return nil, fmt.Errorf("failed to list node events: %w", err)
	}
	defer rows.Close()

	var events []NodeEventRecord
	for rows.Next() {
		var e NodeEventRecord
		if err := rows.Scan(&e.ID, &e.RunID, &e.FlowID, &e.NodeID, &e.NodeType, &e.Phase, &e.MsgID, &e.DurationMs, &e.Error, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan node event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating node events: %w", err)
	}
	return events, nil
}

// PruneHistory deletes runs older than olderThan and, per flow, any runs beyond
// the maxRunsPerFlow most recent. Orphaned node_events are removed in the same
// transaction. It returns the number of runs deleted. maxRunsPerFlow <= 0 skips
// the per-flow cap.
func (s *SQLiteStore) PruneHistory(olderThan time.Time, maxRunsPerFlow int) (int, error) {
	if s.isClosed() {
		return 0, ErrDatabaseClosed
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin prune tx: %w", err)
	}

	total := 0

	res, err := tx.Exec(`DELETE FROM flow_runs WHERE started_at < ?`, olderThan)
	if err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("failed to prune old runs: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil {
		total += int(n)
	}

	if maxRunsPerFlow > 0 {
		res, err = tx.Exec(`
			DELETE FROM flow_runs
			WHERE id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (
						PARTITION BY flow_id ORDER BY started_at DESC
					) AS rn
					FROM flow_runs
				) WHERE rn > ?
			)
		`, maxRunsPerFlow)
		if err != nil {
			_ = tx.Rollback()
			return 0, fmt.Errorf("failed to prune runs over cap: %w", err)
		}
		if n, err := res.RowsAffected(); err == nil {
			total += int(n)
		}
	}

	// Drop node_events whose run no longer exists.
	if _, err := tx.Exec(`DELETE FROM node_events WHERE run_id NOT IN (SELECT id FROM flow_runs)`); err != nil {
		_ = tx.Rollback()
		return 0, fmt.Errorf("failed to prune orphan node events: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit prune: %w", err)
	}
	return total, nil
}
