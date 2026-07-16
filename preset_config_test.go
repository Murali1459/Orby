package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePresetConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connections.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadPresetConfig(t *testing.T) {
	path := writePresetConfig(t, `{
  "profiles": [{
    "id": "local",
    "label": "Local",
    "connections": [{
      "id": "preset:redis-local",
      "name": "redis-local",
      "tool": "redis",
      "host": "127.0.0.1",
      "port": "6379",
      "mode": "single",
      "fields": {"dbIndex": "0"}
    }]
  }]
}`)

	config, err := loadPresetConfig(path, map[string]bool{"redis": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Profiles) != 1 || config.Profiles[0].ID != "local" || config.Profiles[0].Label != "Local" {
		t.Fatalf("profiles = %#v", config.Profiles)
	}
	connection := config.Profiles[0].Connections[0]
	if connection.ID != "preset:redis-local" || connection.Profile != "local" || !connection.Preset || connection.Fields["dbIndex"] != "0" {
		t.Fatalf("connection = %#v", connection)
	}
}

func TestLoadPresetConfigRejectsInvalidConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		content string
		message string
	}{
		{name: "duplicate profile", content: `{"profiles":[{"id":"local","label":"Local"},{"id":"local","label":"Other"}]}`, message: `duplicate profile id "local"`},
		{name: "reserved saved profile", content: `{"profiles":[{"id":"saved","label":"Saved"}]}`, message: `profile id "saved" is reserved`},
		{name: "duplicate connection id", content: `{"profiles":[{"id":"one","label":"One","connections":[{"id":"preset:same","name":"one","tool":"redis","host":"localhost","port":"6379","mode":"single"}]},{"id":"two","label":"Two","connections":[{"id":"preset:same","name":"two","tool":"redis","host":"localhost","port":"6379","mode":"single"}]}]}`, message: `duplicate connection id "preset:same"`},
		{name: "duplicate connection name", content: `{"profiles":[{"id":"one","label":"One","connections":[{"id":"preset:one","name":"same","tool":"redis","host":"localhost","port":"6379","mode":"single"},{"id":"preset:two","name":"same","tool":"redis","host":"localhost","port":"6379","mode":"single"}]}]}`, message: `duplicate connection name "same"`},
		{name: "non preset id", content: `{"profiles":[{"id":"one","label":"One","connections":[{"id":"one","name":"one","tool":"redis","host":"localhost","port":"6379","mode":"single"}]}]}`, message: `must start with "preset:"`},
		{name: "unknown tool", content: `{"profiles":[{"id":"one","label":"One","connections":[{"id":"preset:one","name":"one","tool":"unknown","host":"localhost","port":"6379","mode":"single"}]}]}`, message: `unknown tool "unknown"`},
		{name: "invalid mode", content: `{"profiles":[{"id":"one","label":"One","connections":[{"id":"preset:one","name":"one","tool":"redis","host":"localhost","port":"6379","mode":"other"}]}]}`, message: `invalid mode "other"`},
		{name: "invalid port", content: `{"profiles":[{"id":"one","label":"One","connections":[{"id":"preset:one","name":"one","tool":"redis","host":"localhost","port":"70000","mode":"single"}]}]}`, message: `invalid port "70000"`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := loadPresetConfig(writePresetConfig(t, test.content), map[string]bool{"redis": true})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want containing %q", err, test.message)
			}
		})
	}
}

func TestLoadPresetConfigRejectsMissingFile(t *testing.T) {
	_, err := loadPresetConfig(filepath.Join(t.TempDir(), "missing.json"), map[string]bool{"redis": true})
	if err == nil || !strings.Contains(err.Error(), "load preset connections") {
		t.Fatalf("error = %v", err)
	}
}

func TestBundledPresetConfigPreservesExistingProfiles(t *testing.T) {
	config, err := loadPresetConfig("connections.json", map[string]bool{"aerospike": true, "redis": true})
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	names := map[string]bool{}
	for _, profile := range config.Profiles {
		counts[profile.ID] = len(profile.Connections)
		for _, connection := range profile.Connections {
			names[connection.Name] = true
		}
	}
	if counts["k8-ci"] != 6 || counts["k8-ci-debug"] != 6 || counts["docker"] != 8 || len(names) != 20 {
		t.Fatalf("profile counts = %#v, names = %d", counts, len(names))
	}
	for _, name := range []string{"aerospike-catalog-k8-ci", "aerospike-catalog-k8-ci-debug", "redis-realisation-docker"} {
		if !names[name] {
			t.Fatalf("missing bundled preset %q", name)
		}
	}
}

func TestExamplePresetConfigUsesLocalhostOnly(t *testing.T) {
	config, err := loadPresetConfig("connections.example.json", map[string]bool{"aerospike": true, "redis": true})
	if err != nil {
		t.Fatal(err)
	}
	if len(config.Profiles) != 1 || config.Profiles[0].ID != "local" || len(config.Profiles[0].Connections) != 2 {
		t.Fatalf("config = %#v", config)
	}
	for _, connection := range config.Profiles[0].Connections {
		if connection.Host != "127.0.0.1" {
			t.Fatalf("example connection %q is not local: %q", connection.Name, connection.Host)
		}
	}
}

func TestServerInjectsConfiguredPresetConnections(t *testing.T) {
	path := writePresetConfig(t, `{"profiles":[{"id":"local","label":"Local","connections":[{"id":"preset:redis-local","name":"redis-local","tool":"redis","host":"127.0.0.1","port":"6379","mode":"single"}]}]}`)
	t.Setenv("CONNECTIONS_FILE", path)
	server, err := newServer()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `window.ORBY_PRESET_CONFIG = {"profiles":[{"id":"local"`) || !strings.Contains(response.Body.String(), `"profile":"local"`) {
		t.Fatalf("response = %d %s", response.Code, response.Body.String())
	}
}

func TestServerRejectsInvalidConfiguredPresetConnections(t *testing.T) {
	t.Setenv("CONNECTIONS_FILE", filepath.Join(t.TempDir(), "missing.json"))
	if _, err := newServer(); err == nil || !strings.Contains(err.Error(), "load preset connections") {
		t.Fatalf("error = %v", err)
	}
}
