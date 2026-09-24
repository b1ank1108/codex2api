package database

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteCodexTransportDiagnosticsRoundtrip(t *testing.T) {
	db, err := New("sqlite", filepath.Join(t.TempDir(), "transport-diagnostics.db"))
	if err != nil {
		t.Fatalf("New(sqlite): %v", err)
	}
	defer db.Close()

	payload := []byte(`{"request_id":"req-123","transport":"websocket","stage":"response_stream"}`)
	db.InsertCodexTransportDiagnostic(payload)
	if !db.DrainBackgroundTasks(5 * time.Second) {
		t.Fatal("diagnostic insert did not finish")
	}

	items, err := db.ListCodexTransportDiagnostics(context.Background(), "req-123")
	if err != nil {
		t.Fatalf("ListCodexTransportDiagnostics: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("diagnostics count = %d, want 1", len(items))
	}
	var got map[string]interface{}
	if err := json.Unmarshal(items[0], &got); err != nil {
		t.Fatalf("stored payload is not JSON: %v", err)
	}
	if got["stage"] != "response_stream" {
		t.Fatalf("stored payload = %#v", got)
	}
	if err := db.ClearCodexTransportDiagnostics(context.Background()); err != nil {
		t.Fatalf("ClearCodexTransportDiagnostics: %v", err)
	}
	items, err = db.ListCodexTransportDiagnostics(context.Background(), "req-123")
	if err != nil || len(items) != 0 {
		t.Fatalf("diagnostics after clear = %d, err = %v", len(items), err)
	}
}

func TestSQLiteCodexDiagnosticCaptureSettingRoundtrip(t *testing.T) {
	db, err := New("sqlite", filepath.Join(t.TempDir(), "transport-diagnostics-setting.db"))
	if err != nil {
		t.Fatalf("New(sqlite): %v", err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.conn.ExecContext(ctx, `INSERT INTO system_settings (id) VALUES (1)`); err != nil {
		t.Fatalf("insert defaults: %v", err)
	}
	settings, err := db.GetSystemSettings(ctx)
	if err != nil || settings.CodexDiagnosticCaptureEnabled {
		t.Fatalf("default diagnostic setting = %#v, err = %v", settings, err)
	}
	settings.CodexDiagnosticCaptureEnabled = true
	if err := db.UpdateSystemSettings(ctx, settings); err != nil {
		t.Fatalf("enable diagnostics: %v", err)
	}
	settings, err = db.GetSystemSettings(ctx)
	if err != nil || !settings.CodexDiagnosticCaptureEnabled {
		t.Fatalf("persisted diagnostic setting = %#v, err = %v", settings, err)
	}
}
