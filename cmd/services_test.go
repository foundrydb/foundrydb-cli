package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	foundrydb "github.com/anorph/foundrydb-sdk-go/foundrydb"
	"github.com/spf13/viper"
)

// setupTestServer creates a mock HTTP server and configures viper so that
// newClient() points to it instead of the real API.
func setupTestServer(t *testing.T, mux *http.ServeMux) (*httptest.Server, func()) {
	t.Helper()
	srv := httptest.NewServer(mux)
	viper.Set("api_url", srv.URL)
	viper.Set("username", "test")
	viper.Set("password", "test")
	// Clear any cobra flag overrides so viper values are used exclusively
	apiURL = ""
	username = ""
	password = ""
	orgID = ""
	jsonOut = false

	cleanup := func() {
		srv.Close()
		viper.Reset()
	}
	return srv, cleanup
}

// captureStdout redirects os.Stdout to a pipe, runs fn, then returns the
// captured output. Command handlers use fmt.Println which writes to os.Stdout,
// not to cobra's output writer, so this is required to capture output.
func captureStdout(fn func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// executeCommand runs a cobra sub-command with the given args and captures
// all output written to os.Stdout.
func executeCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var execErr error
	out := captureStdout(func() {
		rootCmd.SetArgs(args)
		execErr = rootCmd.Execute()
	})
	return out, execErr
}

// sampleService returns a representative Service for use in tests.
func sampleService() foundrydb.Service {
	return foundrydb.Service{
		ID:            "abc12345-0000-0000-0000-000000000000",
		Name:          "my-pg",
		DatabaseType:  foundrydb.PostgreSQL,
		Version:       "17",
		Status:        foundrydb.ServiceStatus("running"),
		PlanName:      "tier-2",
		StorageSizeGB: 50,
		StorageTier:   foundrydb.StorageTierMaxIOPS,
		Zone:          "se-sto1",
		CreatedAt:     "2025-01-01T12:00:00Z",
		UpdatedAt:     "2025-01-02T12:00:00Z",
	}
}

// listServicesResponse wraps services for mock server responses.
type listServicesResponse struct {
	Services []foundrydb.Service `json:"services"`
}

// newClientWithURL creates an SDK client pointing at the given URL for tests.
func newClientWithURL(url string) *foundrydb.Client {
	return foundrydb.New(foundrydb.Config{
		APIURL:   url,
		Username: "test",
		Password: "test",
	})
}

// -- services list ------------------------------------------------------------

func TestRunServicesList_Empty(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		json.NewEncoder(w).Encode(listServicesResponse{Services: []foundrydb.Service{}})
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "services", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "No services found") {
		t.Errorf("expected 'No services found', got: %q", out)
	}
}

func TestRunServicesList_WithServices(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		json.NewEncoder(w).Encode(listServicesResponse{
			Services: []foundrydb.Service{svc},
		})
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "services", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "my-pg") {
		t.Errorf("expected service name in output, got: %q", out)
	}
	if !strings.Contains(out, "postgresql") {
		t.Errorf("expected database type in output, got: %q", out)
	}
	if !strings.Contains(out, "running") {
		t.Errorf("expected status in output, got: %q", out)
	}
	if !strings.Contains(out, "Total: 1") {
		t.Errorf("expected total count in output, got: %q", out)
	}
}

func TestRunServicesList_JSONOut(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listServicesResponse{Services: []foundrydb.Service{svc}})
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	jsonOut = true
	defer func() { jsonOut = false }()

	out, err := executeCommand(t, "services", "list")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// SDK encodes Service.ID as "id"
	if !strings.Contains(out, `"id"`) {
		t.Errorf("expected JSON output with 'uuid' field, got: %q", out)
	}
}

func TestRunServicesList_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	_, err := executeCommand(t, "services", "list")
	if err == nil {
		t.Fatal("expected error but got nil")
	}
}

// -- services get -------------------------------------------------------------

func TestRunServicesGet_ByID(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "services", "get", svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, svc.ID) {
		t.Errorf("expected service ID in output, got: %q", out)
	}
	if !strings.Contains(out, "my-pg") {
		t.Errorf("expected service name in output, got: %q", out)
	}
	if !strings.Contains(out, "postgresql 17") {
		t.Errorf("expected database type+version in output, got: %q", out)
	}
	if !strings.Contains(out, "50 GB") {
		t.Errorf("expected storage size in output, got: %q", out)
	}
	if !strings.Contains(out, "se-sto1") {
		t.Errorf("expected zone in output, got: %q", out)
	}
}

func TestRunServicesGet_WithDNS(t *testing.T) {
	svc := sampleService()
	svc.DNSRecords = []foundrydb.DNSRecord{
		{FullDomain: "my-pg.db.foundrydb.com", RecordType: "A", Value: "1.2.3.4"},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "services", "get", svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "DNS Records") {
		t.Errorf("expected DNS Records section, got: %q", out)
	}
	if !strings.Contains(out, "my-pg.db.foundrydb.com") {
		t.Errorf("expected DNS domain in output, got: %q", out)
	}
}

func TestRunServicesGet_JSONOut(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(svc)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	jsonOut = true
	defer func() { jsonOut = false }()

	out, err := executeCommand(t, "services", "get", svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"id"`) {
		t.Errorf("expected JSON output with 'uuid' field, got: %q", out)
	}
}

// -- services create ----------------------------------------------------------

func TestRunServicesCreate_Success(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(svc)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "services", "create",
		"--name", "my-pg",
		"--type", "postgresql",
		"--version", "17",
		"--plan", "tier-2",
		"--zone", "se-sto1",
		"--storage-size", "50",
		"--storage-tier", "maxiops",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Service created successfully") {
		t.Errorf("expected success message, got: %q", out)
	}
	if !strings.Contains(out, svc.ID) {
		t.Errorf("expected service ID in output, got: %q", out)
	}
}

func TestRunServicesCreate_MissingName(t *testing.T) {
	mux := http.NewServeMux()
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	err := runServicesCreate(servicesCreateCmd, []string{})
	_ = err
}

func TestRunServicesCreate_InvalidType(t *testing.T) {
	mux := http.NewServeMux()
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	_, err := executeCommand(t, "services", "create",
		"--name", "my-db",
		"--type", "oracle",
		"--version", "19",
	)
	if err == nil {
		t.Fatal("expected error for invalid database type")
	}
	if !strings.Contains(err.Error(), "invalid database type") {
		t.Errorf("expected 'invalid database type' error, got: %v", err)
	}
}

func TestRunServicesCreate_WithAllowedCIDRs(t *testing.T) {
	svc := sampleService()
	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var buf bytes.Buffer
			buf.ReadFrom(r.Body)
			capturedBody = buf.Bytes()
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(svc)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	_, err := executeCommand(t, "services", "create",
		"--name", "my-pg",
		"--type", "postgresql",
		"--version", "17",
		"--allowed-cidrs", "1.2.3.4/32",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(capturedBody), "1.2.3.4/32") {
		t.Errorf("expected CIDR in request body, got: %s", capturedBody)
	}
}

func TestRunServicesCreate_DefaultVersion(t *testing.T) {
	svc := sampleService()
	var capturedBody []byte
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var buf bytes.Buffer
			buf.ReadFrom(r.Body)
			capturedBody = buf.Bytes()
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(svc)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	_, err := executeCommand(t, "services", "create",
		"--name", "my-kafka",
		"--type", "kafka",
		"--version", "3.9",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(string(capturedBody), "kafka") {
		t.Errorf("expected kafka in request body, got: %s", capturedBody)
	}
}

func TestRunServicesCreate_JSONOut(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(svc)
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	jsonOut = true
	defer func() { jsonOut = false }()

	out, err := executeCommand(t, "services", "create",
		"--name", "my-pg",
		"--type", "postgresql",
		"--version", "17",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"id"`) {
		t.Errorf("expected JSON output with 'uuid' field, got: %q", out)
	}
}

func TestRunServicesCreate_APIError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	_, err := executeCommand(t, "services", "create",
		"--name", "bad-svc",
		"--type", "postgresql",
		"--version", "17",
	)
	if err == nil {
		t.Fatal("expected error from API")
	}
}

// -- services delete ----------------------------------------------------------

func TestRunServicesDelete_WithConfirmFlag(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(svc)
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	out, err := executeCommand(t, "services", "delete", svc.ID, "--confirm")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "has been deleted") {
		t.Errorf("expected deletion message, got: %q", out)
	}
}

func TestRunServicesDelete_APIError(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			json.NewEncoder(w).Encode(svc)
		case http.MethodDelete:
			http.Error(w, "forbidden", http.StatusForbidden)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	_, cleanup := setupTestServer(t, mux)
	defer cleanup()

	_, err := executeCommand(t, "services", "delete", svc.ID, "--confirm")
	if err == nil {
		t.Fatal("expected error from delete API")
	}
}

// -- resolveService -----------------------------------------------------------

func TestResolveService_ByID_Success(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(svc)
	})
	srv, cleanup := setupTestServer(t, mux)
	defer cleanup()

	client := newClientWithURL(srv.URL)
	result, err := resolveService(client, svc.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.ID != svc.ID {
		t.Errorf("expected ID %q, got %q", svc.ID, result.ID)
	}
}

func TestResolveService_ByName_Success(t *testing.T) {
	svc := sampleService()
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/managed-services/my-pg" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(listServicesResponse{Services: []foundrydb.Service{svc}})
	})
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listServicesResponse{Services: []foundrydb.Service{svc}})
	})
	srv, cleanup := setupTestServer(t, mux)
	defer cleanup()

	client := newClientWithURL(srv.URL)
	result, err := resolveService(client, "my-pg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "my-pg" {
		t.Errorf("expected name 'my-pg', got %q", result.Name)
	}
}

func TestResolveService_ByName_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/managed-services/does-not-exist" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(listServicesResponse{Services: []foundrydb.Service{}})
	})
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listServicesResponse{Services: []foundrydb.Service{}})
	})
	srv, cleanup := setupTestServer(t, mux)
	defer cleanup()

	client := newClientWithURL(srv.URL)
	_, err := resolveService(client, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for non-existent service")
	}
	if !strings.Contains(err.Error(), "no service found") {
		t.Errorf("expected 'no service found' error, got: %v", err)
	}
}

func TestResolveService_ByName_MultipleMatches(t *testing.T) {
	svc1 := sampleService()
	svc2 := sampleService()
	svc2.ID = "xyz99999-0000-0000-0000-000000000000"

	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/managed-services/my-pg" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(listServicesResponse{Services: []foundrydb.Service{svc1, svc2}})
	})
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listServicesResponse{Services: []foundrydb.Service{svc1, svc2}})
	})
	srv, cleanup := setupTestServer(t, mux)
	defer cleanup()

	client := newClientWithURL(srv.URL)
	_, err := resolveService(client, "my-pg")
	if err == nil {
		t.Fatal("expected error for multiple matches")
	}
	if !strings.Contains(err.Error(), "multiple services named") {
		t.Errorf("expected 'multiple services named' error, got: %v", err)
	}
}

func TestResolveService_ListError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/managed-services/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	mux.HandleFunc("/managed-services", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	})
	srv, cleanup := setupTestServer(t, mux)
	defer cleanup()

	client := newClientWithURL(srv.URL)
	_, err := resolveService(client, "some-name")
	if err == nil {
		t.Fatal("expected error when both ID lookup and list fail")
	}
	if !strings.Contains(err.Error(), "could not list services") {
		t.Errorf("expected 'could not list services' error, got: %v", err)
	}
}

// -- formatStatus -------------------------------------------------------------

func TestFormatStatus(t *testing.T) {
	tests := []struct {
		input string
	}{
		{"running"},
		{"Running"},
		{"provisioning"},
		{"pending"},
		{"stopped"},
		{"error"},
		{"failed"},
		{"deleting"},
		{"ProvisioningVM"},
		{"unknown-state"},
		{""},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("status_%s", tc.input), func(t *testing.T) {
			got := formatStatus(tc.input)
			if got != tc.input {
				t.Errorf("formatStatus(%q) = %q, want %q (passthrough)", tc.input, got, tc.input)
			}
		})
	}
}

// -- formatBytes --------------------------------------------------------------

func TestFormatBytes(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1024 * 1024, "1.0 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{int64(2.5 * 1024 * 1024 * 1024), "2.5 GB"},
		{1024*1024 - 1, "1024.0 KB"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("bytes_%d", tc.input), func(t *testing.T) {
			got := formatBytes(tc.input)
			if got != tc.expected {
				t.Errorf("formatBytes(%d) = %q, want %q", tc.input, got, tc.expected)
			}
		})
	}
}

// -- printJSON ----------------------------------------------------------------

func TestPrintJSON(t *testing.T) {
	type payload struct {
		Key   string `json:"key"`
		Value int    `json:"value"`
	}
	p := payload{Key: "hello", Value: 42}
	err := printJSON(p)
	if err != nil {
		t.Errorf("printJSON returned error: %v", err)
	}
}
