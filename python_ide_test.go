package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	pluginapi "orby/plugins"
)

const pythonTestPasswordHash = "$2b$04$cZvYlK3TJA/O9lBNMJH0oOz8rTDzYgqB8qXR4KU/6Bl0QDb3EpTuC"

func pythonTestServer(t *testing.T) *server {
	t.Helper()
	pool := newConnectionPool(time.Minute)
	t.Cleanup(pool.Close)
	return &server{
		presets: presetConfig{
			PythonIDE: pythonIDEConfig{Enabled: true, Path: "/hidden-python", Username: "operator", PasswordHash: pythonTestPasswordHash},
			Profiles: []presetProfile{
				{ID: "local", Label: "Local", Connections: []presetConnection{
					{
						ID: "preset:fake", Name: "fake-local", Tool: "fake", Environment: "prod", Profile: "local",
						Host: "127.0.0.1", Port: "3000", Mode: "single", Fields: map[string]string{"locked": "preset"}, Preset: true,
					},
				}},
			},
		},
		plugins:     map[string]pluginapi.Plugin{"fake": &pythonFakePlugin{}},
		connections: pool,
		pythonRuns:  make(chan struct{}, 2),
	}
}

type pythonFakePlugin struct{ request pluginapi.Request }
type pythonFakeConnection struct{ plugin *pythonFakePlugin }

type pythonDeadlineRecorder struct {
	*httptest.ResponseRecorder
	writeDeadlineCleared bool
}

func (recorder *pythonDeadlineRecorder) SetWriteDeadline(deadline time.Time) error {
	recorder.writeDeadlineCleared = deadline.IsZero()
	return nil
}

func (plugin *pythonFakePlugin) Metadata() pluginapi.Metadata {
	return pluginapi.Metadata{Name: "fake", DefaultFormat: "json", Fields: []pluginapi.Field{{Key: "locked"}}}
}
func (plugin *pythonFakePlugin) Connect(pluginapi.Request) (pluginapi.Connection, error) {
	return &pythonFakeConnection{plugin: plugin}, nil
}
func (connection *pythonFakeConnection) Run(request pluginapi.Request) (pluginapi.Result, error) {
	connection.plugin.request = request
	return pluginapi.Result{Tool: "fake", Query: request.Query, Format: "json", JSONValue: request.Fields, HasJSONValue: true, Succeeded: true}, nil
}
func (*pythonFakeConnection) Close() error { return nil }

func TestPythonIDEIsHiddenWhenDisabledAndUsesSignInPage(t *testing.T) {
	server := pythonTestServer(t)
	disabled := pythonTestServer(t)
	disabled.presets.PythonIDE.Enabled = false
	response := httptest.NewRecorder()
	disabled.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/hidden-python", nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("disabled endpoint = %d", response.Code)
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/hidden-python", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "<h1>Sign in</h1>") {
		t.Fatalf("unauthenticated endpoint = %d %s", response.Code, response.Body.String())
	}
	if response.Header().Get("WWW-Authenticate") != "" || strings.Contains(response.Body.String(), `id="source"`) {
		t.Fatalf("unauthenticated endpoint triggered Basic Auth or exposed the editor: headers=%v", response.Header())
	}
	if !strings.Contains(response.Body.String(), `python-login-static-art`) {
		t.Fatal("sign-in page is missing the generated-art background class")
	}
	if strings.Contains(response.Body.String(), `python_logo_3d.js`) || strings.Contains(response.Body.String(), `<canvas`) {
		t.Fatal("sign-in page still includes the 3D rendering path")
	}

	request := httptest.NewRequest(http.MethodGet, "/hidden-python", nil)
	request.SetBasicAuth("operator", "secret")
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Orby Python Workspace") {
		t.Fatalf("authenticated endpoint = %d %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `id="highlight"`) || !strings.Contains(response.Body.String(), "/static/python_ide.js") {
		t.Fatal("authenticated editor is missing Python syntax highlighting")
	}
	for _, expected := range []string{`id="copy-portable"`, `id="portable-connections"`, `127.0.0.1`, `preset:fake`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("authenticated editor is missing portable export data %q", expected)
		}
	}
	for _, expected := range []string{"/static/style.css", "/static/python_ide.css", "operator-brand", `id="run-state"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("authenticated editor is missing Orby theme markup %q", expected)
		}
	}
	for _, expected := range []string{`id="preset-filter"`, `draggable="true"`, `data-preset-id="preset:fake"`} {
		if !strings.Contains(response.Body.String(), expected) {
			t.Fatalf("authenticated editor is missing preset palette markup %q", expected)
		}
	}
	if strings.Contains(response.Body.String(), "secret") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("editor disclosed credentials or is cacheable")
	}
}

func TestPythonIDELogoutEndsAccessAndPasswordLoginRestoresIt(t *testing.T) {
	server := pythonTestServer(t)

	logoutRequest := httptest.NewRequest(http.MethodPost, "/hidden-python/logout", nil)
	logoutRequest.SetBasicAuth("operator", "secret")
	logoutResponse := httptest.NewRecorder()
	server.ServeHTTP(logoutResponse, logoutRequest)
	if logoutResponse.Code != http.StatusSeeOther {
		t.Fatalf("logout = %d %s", logoutResponse.Code, logoutResponse.Body.String())
	}
	var loggedOutCookie *http.Cookie
	for _, cookie := range logoutResponse.Result().Cookies() {
		if cookie.Name == pythonLogoutCookie {
			loggedOutCookie = cookie
		}
	}
	if loggedOutCookie == nil || loggedOutCookie.Value != "1" || !loggedOutCookie.HttpOnly {
		t.Fatalf("logout cookie = %#v", loggedOutCookie)
	}

	blockedRequest := httptest.NewRequest(http.MethodGet, "/hidden-python", nil)
	blockedRequest.SetBasicAuth("operator", "secret")
	blockedRequest.AddCookie(loggedOutCookie)
	blockedResponse := httptest.NewRecorder()
	server.ServeHTTP(blockedResponse, blockedRequest)
	if blockedResponse.Code != http.StatusOK || !strings.Contains(blockedResponse.Body.String(), "<h1>Sign in</h1>") || strings.Contains(blockedResponse.Body.String(), `id="source"`) {
		t.Fatalf("logged-out workspace = %d %s", blockedResponse.Code, blockedResponse.Body.String())
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/hidden-python/login", strings.NewReader("username=operator&password=secret"))
	loginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	loginRequest.AddCookie(loggedOutCookie)
	loginResponse := httptest.NewRecorder()
	server.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusSeeOther {
		t.Fatalf("login = %d %s", loginResponse.Code, loginResponse.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		if cookie.Name == pythonSessionCookie && cookie.Value != "" {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie = %#v", sessionCookie)
	}
	if sessionCookie.MaxAge != int(pythonSessionLifetime/time.Second) {
		t.Fatalf("session lifetime = %d", sessionCookie.MaxAge)
	}

	authenticatedRequest := httptest.NewRequest(http.MethodGet, "/hidden-python", nil)
	authenticatedRequest.AddCookie(sessionCookie)
	authenticatedResponse := httptest.NewRecorder()
	server.ServeHTTP(authenticatedResponse, authenticatedRequest)
	if authenticatedResponse.Code != http.StatusOK || !strings.Contains(authenticatedResponse.Body.String(), `class="python-logout"`) {
		t.Fatalf("session workspace = %d %s", authenticatedResponse.Code, authenticatedResponse.Body.String())
	}

	secondLoginRequest := httptest.NewRequest(http.MethodPost, "/hidden-python/login", strings.NewReader("username=operator&password=secret"))
	secondLoginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	secondLoginRequest.AddCookie(sessionCookie)
	secondLoginResponse := httptest.NewRecorder()
	server.ServeHTTP(secondLoginResponse, secondLoginRequest)
	var secondSessionCookie *http.Cookie
	for _, cookie := range secondLoginResponse.Result().Cookies() {
		if cookie.Name == pythonSessionCookie && cookie.Value != "" {
			secondSessionCookie = cookie
		}
	}
	if secondSessionCookie == nil || secondSessionCookie.Value == sessionCookie.Value {
		t.Fatalf("repeat login reused session token: first=%#v second=%#v", sessionCookie, secondSessionCookie)
	}
	rotatedReplayRequest := httptest.NewRequest(http.MethodGet, "/hidden-python", nil)
	rotatedReplayRequest.AddCookie(sessionCookie)
	rotatedReplayResponse := httptest.NewRecorder()
	server.ServeHTTP(rotatedReplayResponse, rotatedReplayRequest)
	if rotatedReplayResponse.Code != http.StatusOK || !strings.Contains(rotatedReplayResponse.Body.String(), "<h1>Sign in</h1>") || strings.Contains(rotatedReplayResponse.Body.String(), `id="source"`) {
		t.Fatalf("rotated session replay = %d %s", rotatedReplayResponse.Code, rotatedReplayResponse.Body.String())
	}

	independentLoginRequest := httptest.NewRequest(http.MethodPost, "/hidden-python/login", strings.NewReader("username=operator&password=secret"))
	independentLoginRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	independentLoginResponse := httptest.NewRecorder()
	server.ServeHTTP(independentLoginResponse, independentLoginRequest)
	var independentSessionCookie *http.Cookie
	for _, cookie := range independentLoginResponse.Result().Cookies() {
		if cookie.Name == pythonSessionCookie && cookie.Value != "" {
			independentSessionCookie = cookie
		}
	}
	if independentSessionCookie == nil || independentSessionCookie.Value == secondSessionCookie.Value {
		t.Fatalf("independent login reused session token: second=%#v independent=%#v", secondSessionCookie, independentSessionCookie)
	}

	sessionLogoutRequest := httptest.NewRequest(http.MethodPost, "/hidden-python/logout", nil)
	sessionLogoutRequest.AddCookie(secondSessionCookie)
	sessionLogoutResponse := httptest.NewRecorder()
	server.ServeHTTP(sessionLogoutResponse, sessionLogoutRequest)
	if sessionLogoutResponse.Code != http.StatusSeeOther {
		t.Fatalf("session logout = %d %s", sessionLogoutResponse.Code, sessionLogoutResponse.Body.String())
	}

	replayRequest := httptest.NewRequest(http.MethodGet, "/hidden-python", nil)
	replayRequest.AddCookie(secondSessionCookie)
	replayResponse := httptest.NewRecorder()
	server.ServeHTTP(replayResponse, replayRequest)
	if replayResponse.Code != http.StatusOK || !strings.Contains(replayResponse.Body.String(), "<h1>Sign in</h1>") || strings.Contains(replayResponse.Body.String(), `id="source"`) {
		t.Fatalf("revoked session replay = %d %s", replayResponse.Code, replayResponse.Body.String())
	}

	independentRequest := httptest.NewRequest(http.MethodGet, "/hidden-python", nil)
	independentRequest.AddCookie(independentSessionCookie)
	independentResponse := httptest.NewRecorder()
	server.ServeHTTP(independentResponse, independentRequest)
	if independentResponse.Code != http.StatusOK || !strings.Contains(independentResponse.Body.String(), `id="source"`) {
		t.Fatalf("independent session was revoked = %d %s", independentResponse.Code, independentResponse.Body.String())
	}

	key, ok := pythonSessionKey(independentSessionCookie.Value)
	if !ok {
		t.Fatal("second session token is malformed")
	}
	server.pythonSessions.mu.Lock()
	server.pythonSessions.sessions[key] = time.Now().Add(-time.Minute)
	server.pythonSessions.mu.Unlock()
	expiredRequest := httptest.NewRequest(http.MethodGet, "/hidden-python", nil)
	expiredRequest.AddCookie(independentSessionCookie)
	expiredResponse := httptest.NewRecorder()
	server.ServeHTTP(expiredResponse, expiredRequest)
	if expiredResponse.Code != http.StatusOK || !strings.Contains(expiredResponse.Body.String(), "<h1>Sign in</h1>") || strings.Contains(expiredResponse.Body.String(), `id="source"`) {
		t.Fatalf("expired session = %d %s", expiredResponse.Code, expiredResponse.Body.String())
	}
}

func TestPythonIDEConfigurationIsValidatedAndNeverMarshaled(t *testing.T) {
	config := presetConfig{PythonIDE: pythonIDEConfig{Enabled: true, Username: "user", PasswordHash: pythonTestPasswordHash}}
	if err := validatePresetConfig(&config, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if config.PythonIDE.Path != "/_orby/python" {
		t.Fatalf("defaults = %#v", config.PythonIDE)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "pythonIde") {
		t.Fatalf("browser config disclosed Python IDE settings: %s", encoded)
	}

	invalid := presetConfig{PythonIDE: pythonIDEConfig{Enabled: true, Path: "/query", Username: "user", PasswordHash: pythonTestPasswordHash}}
	if err := validatePresetConfig(&invalid, map[string]bool{}); err == nil {
		t.Fatal("expected route conflict to be rejected")
	}
	invalid = presetConfig{PythonIDE: pythonIDEConfig{Enabled: true, Username: "user", PasswordHash: "plaintext"}}
	if err := validatePresetConfig(&invalid, map[string]bool{}); err == nil || !strings.Contains(err.Error(), "valid bcrypt hash") {
		t.Fatalf("plaintext passwordHash was not rejected: %v", err)
	}
}

func TestPythonPortableExportIncludesResolvedConnections(t *testing.T) {
	server := pythonTestServer(t)
	connection := &server.presets.Profiles[0].Connections[0]
	connection.Tool = "redis"
	connection.Fields = map[string]string{"dbIndex": "4"}
	var connections []pythonPortableConnection
	if err := json.Unmarshal([]byte(server.pythonPortableConnections()), &connections); err != nil {
		t.Fatal(err)
	}
	if len(connections) != 1 || connections[0].ID != "preset:fake" || connections[0].Hosts[0].Host != "127.0.0.1" || connections[0].Hosts[0].Port != 3000 || connections[0].Fields["dbIndex"] != "4" {
		t.Fatalf("portable connections = %#v", connections)
	}
}

func TestPythonIDEExecutesScriptWithPresetClientBridge(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	server := pythonTestServer(t)
	body := `{"script":"from orby import client, connections\nprint(connections()[0]['name'])\nprint(client('fake-local').run('LOOKUP', locked='forged', value=42))"}`
	request := httptest.NewRequest(http.MethodPost, "/hidden-python", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.SetBasicAuth("operator", "secret")
	response := &pythonDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("run = %d %s", response.Code, response.Body.String())
	}
	var result pythonRunResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Output, "fake-local") || !strings.Contains(result.Output, "'value': '42'") {
		t.Fatalf("output = %q error=%q", result.Output, result.Error)
	}
	plugin := server.plugins["fake"].(*pythonFakePlugin)
	if plugin.request.Fields["locked"] != "preset" {
		t.Fatalf("connection field was overridden: %#v", plugin.request.Fields)
	}
	if plugin.request.Environment != "prod" || plugin.request.Host != "127.0.0.1" {
		t.Fatalf("preset identity was not authoritative: %#v", plugin.request)
	}
}

func TestPythonIDEShowsOnlyScriptExceptionMessage(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	server := pythonTestServer(t)
	result, status := server.executePython(t.Context(), `raise RuntimeError("database unavailable")`)
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d result=%#v", status, result)
	}
	if strings.TrimSpace(result.Output) != "database unavailable" {
		t.Fatalf("output = %q", result.Output)
	}
	if result.Error != "" || strings.Contains(result.Output, "Traceback") {
		t.Fatalf("unexpected wrapper error or traceback: %#v", result)
	}
}

func TestPythonIDEStreamsOutputAsSSE(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	server := pythonTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/hidden-python", strings.NewReader(`{"script":"print('live')"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	request.SetBasicAuth("operator", "secret")
	response := &pythonDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream = %d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
	if !response.writeDeadlineCleared {
		t.Fatal("script request retained the HTTP server write deadline")
	}
	body := response.Body.String()
	for _, expected := range []string{"event: start", "event: output", "event: done", `"ok":true`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("SSE stream is missing %q: %s", expected, body)
		}
	}
	var streamed []byte
	for line := range strings.SplitSeq(body, "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var payload struct {
			ChunkBase64 string `json:"chunkBase64"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload) != nil || payload.ChunkBase64 == "" {
			continue
		}
		chunk, err := base64.StdEncoding.DecodeString(payload.ChunkBase64)
		if err != nil {
			t.Fatalf("invalid streamed chunk %q: %v", payload.ChunkBase64, err)
		}
		streamed = append(streamed, chunk...)
	}
	if string(streamed) != "live\n" {
		t.Fatalf("streamed output = %q", streamed)
	}
}
