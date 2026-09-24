package database

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"
)

var codexTransportDiagnosticInsertCount atomic.Uint64

// InsertCodexTransportDiagnostic persists one already-redacted transport capture.
// The write is asynchronous so diagnostics never add database latency to a relay.
func (db *DB) InsertCodexTransportDiagnostic(payload []byte) {
	if db == nil || len(payload) == 0 {
		return
	}
	var envelope struct {
		RequestID  string    `json:"request_id"`
		Transport  string    `json:"transport"`
		CapturedAt time.Time `json:"captured_at"`
	}
	if json.Unmarshal(payload, &envelope) != nil || envelope.RequestID == "" {
		return
	}
	payload = append([]byte(nil), payload...)
	db.RunBackgroundTask(func(taskCtx context.Context) {
		ctx, cancel := context.WithTimeout(taskCtx, 3*time.Second)
		defer cancel()
		capturedAt := envelope.CapturedAt
		if capturedAt.IsZero() {
			capturedAt = time.Now().UTC()
		}
		if db.driver == "sqlite" {
			_, _ = db.conn.ExecContext(ctx, `INSERT INTO codex_transport_diagnostics (request_id, captured_at, transport, payload) VALUES (?, ?, ?, ?)`, envelope.RequestID, capturedAt, envelope.Transport, string(payload))
		} else {
			_, _ = db.conn.ExecContext(ctx, `INSERT INTO codex_transport_diagnostics (request_id, captured_at, transport, payload) VALUES ($1, $2, $3, $4::jsonb)`, envelope.RequestID, capturedAt, envelope.Transport, string(payload))
		}
		insertCount := codexTransportDiagnosticInsertCount.Add(1)
		if insertCount == 1 || insertCount%128 == 0 {
			db.deleteExpiredCodexTransportDiagnostics(ctx)
		}
	})
}

// ListCodexTransportDiagnostics returns every captured attempt for one gateway request.
func (db *DB) ListCodexTransportDiagnostics(ctx context.Context, requestID string) ([]json.RawMessage, error) {
	query := `SELECT payload FROM codex_transport_diagnostics WHERE request_id = ? ORDER BY captured_at, id`
	args := []interface{}{requestID}
	if db.driver != "sqlite" {
		query = `SELECT payload::text FROM codex_transport_diagnostics WHERE request_id = $1 ORDER BY captured_at, id`
	}
	rows, err := db.conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]json.RawMessage, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		result = append(result, json.RawMessage(payload))
	}
	return result, rows.Err()
}

func (db *DB) ClearCodexTransportDiagnostics(ctx context.Context) error {
	_, err := db.conn.ExecContext(ctx, `DELETE FROM codex_transport_diagnostics`)
	return err
}

func (db *DB) deleteExpiredCodexTransportDiagnostics(ctx context.Context) {
	if db.driver == "sqlite" {
		_, _ = db.conn.ExecContext(ctx, `DELETE FROM codex_transport_diagnostics WHERE captured_at < datetime('now', '-7 days')`)
		return
	}
	_, _ = db.conn.ExecContext(ctx, `DELETE FROM codex_transport_diagnostics WHERE captured_at < NOW() - INTERVAL '7 days'`)
}

func (db *DB) loadCodexDiagnosticCaptureSetting(ctx context.Context, settings *SystemSettings) {
	if db == nil || settings == nil {
		return
	}
	_ = db.conn.QueryRowContext(ctx, `SELECT COALESCE(codex_diagnostic_capture_enabled, false) FROM system_settings WHERE id = 1`).Scan(&settings.CodexDiagnosticCaptureEnabled)
}

func (db *DB) saveCodexDiagnosticCaptureSetting(ctx context.Context, enabled bool) error {
	if db.driver == "sqlite" {
		_, err := db.conn.ExecContext(ctx, `INSERT INTO system_settings (id, codex_diagnostic_capture_enabled) VALUES (1, ?) ON CONFLICT(id) DO UPDATE SET codex_diagnostic_capture_enabled = excluded.codex_diagnostic_capture_enabled`, enabled)
		return err
	}
	_, err := db.conn.ExecContext(ctx, `INSERT INTO system_settings (id, codex_diagnostic_capture_enabled) VALUES (1, $1) ON CONFLICT(id) DO UPDATE SET codex_diagnostic_capture_enabled = EXCLUDED.codex_diagnostic_capture_enabled`, enabled)
	return err
}
