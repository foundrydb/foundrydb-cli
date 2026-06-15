package cmd

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
)

func sampleBackups() []foundrydb.Backup {
	size1 := int64(10 * 1024 * 1024) // 10 MB
	return []foundrydb.Backup{
		{
			ID:         "bkp12345-0000-0000-0000-000000000000",
			BackupType: foundrydb.BackupTypeFull,
			Status:     foundrydb.BackupStatusCompleted,
			SizeBytes:  &size1,
			CreatedAt:  "2025-01-01T12:00:00Z",
		},
		{
			ID:         "bkp99999-0000-0000-0000-000000000000",
			BackupType: foundrydb.BackupTypeIncremental,
			Status:     foundrydb.BackupStatus("in_progress"),
			SizeBytes:  nil,
			CreatedAt:  "2025-01-02T12:00:00Z",
		},
	}
}

// listBackupsResponse wraps backups for mock server responses.
type listBackupsResponse struct {
	Backups []foundrydb.Backup `json:"backups"`
}

func TestRunBackupsList_WithBackups(t *testing.T) {
	svc := sampleService()
	backups := sampleBackups()

	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/backups") {
			json.NewEncoder(w).Encode(listBackupsResponse{Backups: backups})
			return
		}
		// Service lookup
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "backups", "list", svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "full") {
		t.Errorf("expected backup type in output, got: %q", out)
	}
	if !strings.Contains(out, "completed") {
		t.Errorf("expected backup status in output, got: %q", out)
	}
	if !strings.Contains(out, "10.0 MB") {
		t.Errorf("expected formatted size in output, got: %q", out)
	}
	// Zero-size backup should show "-"
	if !strings.Contains(out, "-") {
		t.Errorf("expected '-' for zero-size backup, got: %q", out)
	}
	if !strings.Contains(out, "Total: 2") {
		t.Errorf("expected total count, got: %q", out)
	}
}

func TestRunBackupsList_Empty(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/backups") {
			json.NewEncoder(w).Encode(listBackupsResponse{Backups: []foundrydb.Backup{}})
			return
		}
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "backups", "list", svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No backups found") {
		t.Errorf("expected 'No backups found', got: %q", out)
	}
}

func TestRunBackupsList_JSONOut(t *testing.T) {
	svc := sampleService()
	backups := sampleBackups()

	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/backups") {
			json.NewEncoder(w).Encode(listBackupsResponse{Backups: backups})
			return
		}
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	jsonOut = true
	defer func() { jsonOut = false }()

	out, err := executeCommand(t, "backups", "list", svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SDK Backup type uses "backup_type" field name
	if !strings.Contains(out, `"backup_type"`) {
		t.Errorf("expected JSON output with 'backup_type', got: %q", out)
	}
}

func TestRunBackupsTrigger_Success(t *testing.T) {
	svc := sampleService()
	triggerResp := map[string]string{"backup_id": "bkp-new-001", "status": "pending"}

	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/backups") {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(triggerResp)
			return
		}
		// Service lookup
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "backups", "trigger", svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Backup triggered successfully") {
		t.Errorf("expected success message, got: %q", out)
	}
	if !strings.Contains(out, "bkp-new-001") {
		t.Errorf("expected backup ID in output, got: %q", out)
	}
}

func TestRunBackupsTrigger_JSONOut(t *testing.T) {
	svc := sampleService()
	triggerResp := map[string]string{"backup_id": "bkp-new-001", "status": "pending"}

	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/backups") {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(triggerResp)
			return
		}
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	jsonOut = true
	defer func() { jsonOut = false }()

	out, err := executeCommand(t, "backups", "trigger", svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"id"`) {
		t.Errorf("expected JSON output, got: %q", out)
	}
}

func TestRunBackupsTrigger_APIError(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/backups") {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	_, err := executeCommand(t, "backups", "trigger", svc.ID)
	if err == nil {
		t.Fatal("expected error from trigger API")
	}
}
