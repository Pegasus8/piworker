package storage

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrFlowRunNotFound is returned when a run does not exist in the given flow.
var ErrFlowRunNotFound = errors.New("flow run not found")

const flowRunColumns = `id, flow_id, started_at, finished_at, status, node_count, error_count, actor`

func scanFlowRun(row interface{ Scan(...any) error }) (FlowRun, error) {
	var r FlowRun
	err := row.Scan(&r.ID, &r.FlowID, &r.StartedAt, &r.FinishedAt, &r.Status, &r.NodeCount, &r.ErrorCount, &r.Actor)
	return r, err
}

// GetFlowRun returns one run, but only if it belongs to flowID — so a run
// can't be read through another flow.
func (s *SQLiteStore) GetFlowRun(flowID, runID string) (*FlowRun, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	r, err := scanFlowRun(s.db.QueryRow(`SELECT `+flowRunColumns+` FROM flow_runs WHERE id = ? AND flow_id = ?`, runID, flowID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrFlowRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get flow run: %w", err)
	}
	return &r, nil
}

// ListFlowRunsBefore returns up to limit runs of a flow, newest first (ties
// on the start time broken by run ID, so the order is total). With a
// beforeRunID it returns the runs that come after that run in this order,
// which pages through the history without offsets; a beforeRunID that is not
// a run of flowID returns ErrFlowRunNotFound.
func (s *SQLiteStore) ListFlowRunsBefore(flowID, beforeRunID string, limit int) ([]FlowRun, error) {
	if s.isClosed() {
		return nil, ErrDatabaseClosed
	}
	var (
		rows *sql.Rows
		err  error
	)
	if beforeRunID == "" {
		rows, err = s.db.Query(`
			SELECT `+flowRunColumns+`
			FROM flow_runs
			WHERE flow_id = ?
			ORDER BY started_at DESC, id DESC
			LIMIT ?
		`, flowID, limit)
	} else {
		var one int
		err = s.db.QueryRow(`SELECT 1 FROM flow_runs WHERE id = ? AND flow_id = ?`, beforeRunID, flowID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFlowRunNotFound
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read the run cursor: %w", err)
		}
		// Compare with the cursor's stored start time (not a re-encoded
		// one), exactly as ORDER BY compares the rows.
		rows, err = s.db.Query(`
			SELECT `+flowRunColumns+`
			FROM flow_runs
			WHERE flow_id = ?
				AND (started_at < (SELECT started_at FROM flow_runs WHERE id = ?)
					OR (started_at = (SELECT started_at FROM flow_runs WHERE id = ?) AND id < ?))
			ORDER BY started_at DESC, id DESC
			LIMIT ?
		`, flowID, beforeRunID, beforeRunID, beforeRunID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list flow runs: %w", err)
	}
	defer rows.Close()

	var runs []FlowRun
	for rows.Next() {
		r, err := scanFlowRun(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan flow run: %w", err)
		}
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating flow runs: %w", err)
	}
	return runs, nil
}
