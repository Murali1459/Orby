package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"maps"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/crypto/bcrypt"

	pluginapi "orby/plugins"
)

const (
	maxPythonSourceBytes  = 128 << 10
	maxPythonOutputBytes  = 1 << 20
	maxBridgeBytes        = 2 << 20
	pythonSessionCookie   = "orby_python_session"
	pythonLogoutCookie    = "orby_python_logged_out"
	pythonSessionBytes    = 32
	pythonSessionLifetime = 12 * time.Hour
)

type pythonSessionStore struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]time.Time
}

type pythonConnectionView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Tool        string `json:"tool"`
	Environment string `json:"environment"`
	Profile     string `json:"profile"`
	Badge       string `json:"badge"`
	ColorClass  string `json:"colorClass"`
}

type pythonPageData struct {
	Connections         []pythonConnectionView
	LogoutPath          string
	PortableConnections string
}

type pythonRunRequest struct {
	Script string `json:"script"`
}

type pythonRunResponse struct {
	Output    string `json:"output"`
	Error     string `json:"error,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type pythonBridgeRequest struct {
	Connection string            `json:"connection"`
	Query      string            `json:"query"`
	Format     string            `json:"format"`
	Fields     map[string]string `json:"fields"`
}

type pythonBridgeResponse struct {
	OK         bool   `json:"ok"`
	Error      string `json:"error,omitempty"`
	Tool       string `json:"tool,omitempty"`
	Query      string `json:"query,omitempty"`
	Format     string `json:"format,omitempty"`
	Data       any    `json:"data,omitempty"`
	Count      *int   `json:"count,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

var pythonPageTemplate = template.Must(template.New("python-ide").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Orby Python Workspace</title><link rel="icon" href="/static/icons/orby.svg" type="image/svg+xml"><style>
:root{color-scheme:dark;font:14px ui-monospace,SFMono-Regular,Menlo,monospace;background:#090d13;color:#dce5f0}*{box-sizing:border-box}body{margin:0;height:100vh;display:grid;grid-template-rows:auto 1fr auto;background:radial-gradient(circle at top,#152033,#090d13 55%)}header{display:flex;align-items:center;gap:14px;padding:13px 18px;border-bottom:1px solid #263244;background:#0d131d}header strong{font-size:16px;color:#fff}header span{color:#8290a4;font-size:12px}.workspace{display:grid;grid-template-columns:230px minmax(360px,1fr) minmax(320px,.8fr);min-height:0}.pane{display:flex;flex-direction:column;min-width:0;min-height:0}.pane+ .pane{border-left:1px solid #263244}.pane-label{padding:9px 12px;color:#91a0b5;border-bottom:1px solid #263244;background:#0c121b}.pane-label small{float:right;color:#66758a}.preset-pane{display:flex;flex-direction:column;min-width:0;min-height:0;border-right:1px solid #263244;background:#0b111a}.preset-search{padding:9px;border-bottom:1px solid #202b3b}.preset-search input{width:100%;border:1px solid #2b3a50;border-radius:5px;padding:7px 9px;background:#0f1722;color:#dce5f0;font:inherit;outline:none}.preset-search input:focus{border-color:#5b86df;box-shadow:0 0 0 2px #315b9c55}.preset-list{overflow:auto;padding:7px;display:flex;flex-direction:column;gap:5px}.preset-empty{padding:18px 8px;color:#66758a;text-align:center}.preset-item{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:8px;width:100%;border:1px solid transparent;border-radius:6px;padding:8px;background:#101925;color:#dce5f0;text-align:left;cursor:grab}.preset-item:hover,.preset-item:focus-visible{border-color:#3b5d91;background:#142135;outline:none}.preset-item.is-dragging{opacity:.5}.preset-badge{min-width:30px;border-radius:4px;padding:3px 5px;background:#21324a;color:#9cc0ff;font-size:10px;text-align:center;text-transform:uppercase}.preset-copy{min-width:0}.preset-copy strong,.preset-copy small{display:block;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.preset-copy strong{font-size:12px}.preset-copy small{margin-top:3px;color:#718097;font-size:10px}.preset-handle{color:#56667d;letter-spacing:-2px}.editor{position:relative;flex:1;min-height:0;background:#090d13}.editor.is-drag-over{box-shadow:inset 0 0 0 2px #5b86df}.editor textarea,.editor-highlight,#output{margin:0;width:100%;border:0;outline:0;padding:18px;font:14px/1.55 ui-monospace,SFMono-Regular,Menlo,monospace;tab-size:4}.editor textarea,.editor-highlight{position:absolute;inset:0;height:100%;resize:none;overflow:auto;white-space:pre;word-wrap:normal}.editor-highlight{pointer-events:none;color:#dce5f0;background:#090d13;scrollbar-width:none}.editor-highlight::-webkit-scrollbar{display:none}.editor textarea{z-index:1;background:transparent;color:transparent;-webkit-text-fill-color:transparent;caret-color:#fff}.editor textarea::selection{background:#315b9c99}.tok-comment{color:#637083;font-style:italic}.tok-string{color:#a7d88d}.tok-keyword{color:#c792ea;font-weight:600}.tok-builtin{color:#82aaff}.tok-number{color:#f6c177}.tok-decorator{color:#f0a36b}#output{flex:1;overflow:auto;white-space:pre-wrap;background:#090d13;color:#b9f6ca}footer{display:flex;align-items:center;gap:12px;padding:11px 16px;border-top:1px solid #263244;background:#0d131d}button{border:1px solid #4676d7;border-radius:6px;padding:8px 18px;background:#2459c4;color:#fff;font:inherit;cursor:pointer}button:disabled{opacity:.55;cursor:wait}footer span{color:#8290a4;font-size:12px}code{color:#8cb4ff}@media(max-width:900px){.workspace{grid-template-columns:1fr;grid-template-rows:auto minmax(260px,1fr) minmax(220px,1fr)}.preset-pane{max-height:180px;border-right:0;border-bottom:1px solid #263244}.preset-list{display:grid;grid-template-columns:repeat(auto-fill,minmax(190px,1fr))}.pane+ .pane{border-left:0;border-top:1px solid #263244}}@media(forced-colors:active){.editor-highlight{display:none}.editor textarea{color:CanvasText;-webkit-text-fill-color:CanvasText}}
</style><link rel="stylesheet" href="/static/style.css"><link rel="stylesheet" href="/static/python_ide.css"></head><body><header class="python-topbar"><a class="operator-brand" href="/" aria-label="Back to Orby workbench"><span class="brand-mark" aria-hidden="true"><img src="/static/icons/orby.svg" alt=""></span><span class="brand-copy"><strong>Orby</strong><small>query me maybe</small></span></a><span class="python-context">Python workspace</span><div class="python-runtime"><span class="auth-badge">Authenticated</span><span id="run-state" data-state="idle"><span class="status-dot"></span><span id="run-state-text">Ready</span></span><form method="post" action="{{.LogoutPath}}"><button class="python-logout" type="submit">Logout</button></form></div></header>
<main class="workspace"><aside class="preset-pane" aria-label="Preset clients"><div class="pane-label">Preset clients <small>{{len .Connections}}</small></div><label class="preset-search"><input id="preset-filter" type="search" placeholder="Filter presets…" autocomplete="off" aria-label="Filter presets"></label><div id="preset-list" class="preset-list">{{range .Connections}}<button type="button" class="preset-item" draggable="true" data-preset-id="{{.ID}}" data-preset-name="{{.Name}}" title="Drag into the script or click to insert"><span class="tool-badge {{.ColorClass}}">{{.Badge}}</span><span class="preset-copy"><strong>{{.Name}}</strong><small>{{.Profile}} · {{.Environment}}</small></span><span class="preset-handle" aria-hidden="true">⋮⋮</span></button>{{else}}<p class="preset-empty">No preset connections configured.</p>{{end}}</div></aside><section class="pane"><div class="pane-label"><span>script.py</span><small>Python 3</small></div><div class="editor"><pre id="highlight" class="editor-highlight" aria-hidden="true"></pre><textarea id="source" spellcheck="false" autocomplete="off" autocapitalize="off">from orby import client, connections

print(connections())

# Redis example:
# redis = client("preset:redis-local")
# print(redis.execute("GET", "my-key"))

# Aerospike example:
# aerospike = client("preset:aerospike-local")
# print(aerospike.run(namespace="test", set="demo", primaryKey="key-1"))
</textarea></div></section><section class="pane output-pane"><div class="pane-label"><span>Live output</span><small>SSE</small></div><pre id="output" data-state="idle">Ready.</pre></section></main>
<footer class="python-footer"><span>Drag a preset into the editor or click it to insert a client.</span><textarea id="portable-connections" hidden readonly>{{.PortableConnections}}</textarea><button id="copy-portable" class="python-copy" type="button">Copy portable</button><span class="run-shortcut"><kbd>⌘</kbd><kbd>Enter</kbd></span><button id="run" type="button">Run script</button></footer>
<script type="module" src="/static/python_ide.js"></script></body></html>`))

var pythonLoginTemplate = template.Must(template.New("python-login").Parse(`<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Sign in · Orby Python Workspace</title><link rel="icon" href="/static/icons/orby.svg" type="image/svg+xml"><link rel="stylesheet" href="/static/style.css"><link rel="stylesheet" href="/static/python_ide.css"></head>
<body class="python-login-body python-login-static-art"><main class="python-login-card"><a class="operator-brand" href="/" aria-label="Back to Orby workbench"><span class="brand-mark" aria-hidden="true"><img src="/static/icons/orby.svg" alt=""></span><span class="brand-copy"><strong>Orby</strong><small>query me maybe</small></span></a><div><p class="python-login-eyebrow">Python workspace</p><h1>Sign in</h1><p>Use your configured workspace credentials.</p></div>{{if .Error}}<p class="python-login-error" role="alert">{{.Error}}</p>{{end}}<form method="post" action="{{.LoginPath}}"><label>Username<input name="username" autocomplete="username" required autofocus></label><label>Password<input name="password" type="password" autocomplete="current-password" required></label><button type="submit">Sign in</button></form></main></body></html>`))

type pythonLoginData struct {
	LoginPath string
	Error     string
}

func (server *server) pythonIDE(writer http.ResponseWriter, request *http.Request) {
	setPythonSecurityHeaders(writer)
	basePath := server.presets.PythonIDE.Path
	switch request.URL.Path {
	case basePath + "/logout":
		server.pythonLogout(writer, request)
		return
	case basePath + "/login":
		server.pythonLogin(writer, request)
		return
	}
	if !server.pythonAuthenticated(request) {
		if request.Method == http.MethodGet {
			server.renderPythonLogin(writer, http.StatusOK, "")
			return
		}
		writeJSON(writer, http.StatusUnauthorized, pythonRunResponse{Error: "authentication required"})
		return
	}
	switch request.Method {
	case http.MethodGet:
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := pythonPageTemplate.Execute(writer, pythonPageData{Connections: server.pythonConnectionViews(), LogoutPath: basePath + "/logout", PortableConnections: server.pythonPortableConnections()}); err != nil {
			http.Error(writer, "unable to render Python workspace", http.StatusInternalServerError)
		}
	case http.MethodPost:
		server.runPythonRequest(writer, request)
	default:
		writer.Header().Set("Allow", "GET, POST")
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (server *server) pythonLogout(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	if cookie, err := request.Cookie(pythonSessionCookie); err == nil {
		server.pythonSessions.revoke(cookie.Value)
	}
	server.setPythonCookie(writer, request, pythonSessionCookie, "", -1)
	server.setPythonCookie(writer, request, pythonLogoutCookie, "1", 0)
	http.Redirect(writer, request, server.presets.PythonIDE.Path, http.StatusSeeOther)
}

func (server *server) pythonLogin(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, 8<<10)
	if err := request.ParseForm(); err != nil {
		server.renderPythonLogin(writer, http.StatusBadRequest, "Unable to read those credentials.")
		return
	}
	if !server.pythonCredentialsMatch(request.PostForm.Get("username"), request.PostForm.Get("password")) {
		server.renderPythonLogin(writer, http.StatusUnauthorized, "Username or password is incorrect.")
		return
	}
	session, err := server.pythonSessions.create(time.Now())
	if err != nil {
		server.renderPythonLogin(writer, http.StatusInternalServerError, "Unable to create a secure session.")
		return
	}
	if previous, cookieErr := request.Cookie(pythonSessionCookie); cookieErr == nil && previous.Value != session {
		server.pythonSessions.revoke(previous.Value)
	}
	server.setPythonCookie(writer, request, pythonLogoutCookie, "", -1)
	server.setPythonCookie(writer, request, pythonSessionCookie, session, int(pythonSessionLifetime/time.Second))
	http.Redirect(writer, request, server.presets.PythonIDE.Path, http.StatusSeeOther)
}

func (server *server) renderPythonLogin(writer http.ResponseWriter, status int, message string) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.WriteHeader(status)
	_ = pythonLoginTemplate.Execute(writer, pythonLoginData{LoginPath: server.presets.PythonIDE.Path + "/login", Error: message})
}

func (server *server) setPythonCookie(writer http.ResponseWriter, request *http.Request, name, value string, maxAge int) {
	http.SetCookie(writer, &http.Cookie{
		Name: name, Value: value, Path: server.presets.PythonIDE.Path,
		HttpOnly: true, Secure: request.TLS != nil, SameSite: http.SameSiteStrictMode, MaxAge: maxAge,
	})
}

func setPythonSecurityHeaders(writer http.ResponseWriter) {
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; img-src 'self'; script-src 'self'; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
	writer.Header().Set("Referrer-Policy", "no-referrer")
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("X-Frame-Options", "DENY")
	writer.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
}

func (server *server) pythonAuthenticated(request *http.Request) bool {
	if server.pythonLoggedOut(request) {
		return false
	}
	if cookie, err := request.Cookie(pythonSessionCookie); err == nil {
		return server.pythonSessions.valid(cookie.Value, time.Now())
	}
	username, password, ok := request.BasicAuth()
	if !ok {
		return false
	}
	return server.pythonCredentialsMatch(username, password)
}

func (server *server) pythonLoggedOut(request *http.Request) bool {
	cookie, err := request.Cookie(pythonLogoutCookie)
	return err == nil && cookie.Value == "1"
}

func (server *server) pythonCredentialsMatch(username, password string) bool {
	config := server.presets.PythonIDE
	usernameHash, configuredUsernameHash := sha256.Sum256([]byte(username)), sha256.Sum256([]byte(config.Username))
	userOK := subtle.ConstantTimeCompare(usernameHash[:], configuredUsernameHash[:]) == 1
	passwordOK := bcrypt.CompareHashAndPassword([]byte(config.PasswordHash), []byte(password)) == nil
	return userOK && passwordOK
}

func (sessions *pythonSessionStore) create(now time.Time) (string, error) {
	raw := make([]byte, pythonSessionBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	key := sha256.Sum256(raw)
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if sessions.sessions == nil {
		sessions.sessions = make(map[[sha256.Size]byte]time.Time)
	}
	for existing, expiry := range sessions.sessions {
		if !expiry.After(now) {
			delete(sessions.sessions, existing)
		}
	}
	sessions.sessions[key] = now.Add(pythonSessionLifetime)
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (sessions *pythonSessionStore) valid(token string, now time.Time) bool {
	key, ok := pythonSessionKey(token)
	if !ok {
		return false
	}
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	expires, ok := sessions.sessions[key]
	if !ok || !expires.After(now) {
		delete(sessions.sessions, key)
		return false
	}
	return true
}

func (sessions *pythonSessionStore) revoke(token string) {
	key, ok := pythonSessionKey(token)
	if !ok {
		return
	}
	sessions.mu.Lock()
	delete(sessions.sessions, key)
	sessions.mu.Unlock()
}

func pythonSessionKey(token string) ([sha256.Size]byte, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != pythonSessionBytes {
		return [sha256.Size]byte{}, false
	}
	return sha256.Sum256(raw), true
}

func (server *server) pythonConnectionViews() []pythonConnectionView {
	var views []pythonConnectionView
	for _, profile := range server.presets.Profiles {
		for _, connection := range profile.Connections {
			metadata := server.plugins[connection.Tool].Metadata()
			views = append(views, pythonConnectionView{
				ID: connection.ID, Name: connection.Name, Tool: connection.Tool,
				Environment: connection.Environment, Profile: connection.Profile,
				Badge: metadata.Badge, ColorClass: metadata.ColorClass,
			})
		}
	}
	return views
}

func (server *server) runPythonRequest(writer http.ResponseWriter, request *http.Request) {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeJSON(writer, http.StatusUnsupportedMediaType, pythonRunResponse{Error: "Content-Type must be application/json"})
		return
	}
	request.Body = http.MaxBytesReader(writer, request.Body, maxPythonSourceBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	var input pythonRunRequest
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, pythonRunResponse{Error: "invalid request: " + err.Error()})
		return
	}
	if strings.TrimSpace(input.Script) == "" {
		writeJSON(writer, http.StatusBadRequest, pythonRunResponse{Error: "script cannot be empty"})
		return
	}
	// Script execution has no configured deadline. Clear the HTTP server's
	// general response-write deadline for this request as well, otherwise a
	// healthy long-running script would lose its response after 60 seconds.
	_ = http.NewResponseController(writer).SetWriteDeadline(time.Time{})
	select {
	case server.pythonRuns <- struct{}{}:
		defer func() { <-server.pythonRuns }()
	default:
		writeJSON(writer, http.StatusTooManyRequests, pythonRunResponse{Error: "too many Python scripts are already running"})
		return
	}
	if strings.Contains(request.Header.Get("Accept"), "text/event-stream") {
		server.streamPythonResult(writer, request, input.Script)
		return
	}
	result, status := server.executePython(request.Context(), input.Script)
	writeJSON(writer, status, result)
}

func (server *server) executePython(parent context.Context, source string) (pythonRunResponse, int) {
	return server.executePythonWithOutput(parent, source, nil)
}

func (server *server) streamPythonResult(writer http.ResponseWriter, request *http.Request, source string) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeJSON(writer, http.StatusInternalServerError, pythonRunResponse{Error: "streaming is unavailable"})
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	writePythonSSE(writer, flusher, "start", map[string]any{})
	result, status := server.executePythonWithOutput(request.Context(), source, func(chunk string) {
		writePythonSSE(writer, flusher, "output", map[string]string{"chunkBase64": base64.StdEncoding.EncodeToString([]byte(chunk))})
	})
	if result.Error != "" {
		writePythonSSE(writer, flusher, "error", map[string]string{"message": result.Error})
	}
	writePythonSSE(writer, flusher, "done", map[string]any{
		"ok": status == http.StatusOK, "status": status,
		"truncated": result.Truncated,
	})
}

func writePythonSSE(writer io.Writer, flusher http.Flusher, event string, value any) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", event, encoded)
	flusher.Flush()
}

func (server *server) executePythonWithOutput(parent context.Context, source string, onOutput func(string)) (pythonRunResponse, int) {
	python, err := exec.LookPath("python3")
	if err != nil {
		return pythonRunResponse{Error: "python3 is not installed on the Orby server"}, http.StatusServiceUnavailable
	}
	temporary, err := os.MkdirTemp("", "orby-python-")
	if err != nil {
		return pythonRunResponse{Error: "unable to prepare Python workspace"}, http.StatusInternalServerError
	}
	defer os.RemoveAll(temporary)
	scriptPath := temporary + "/runner.py"
	wrapper := pythonPrelude(server.pythonConnectionViews()) + `
try:
    exec(compile(` + strconv.Quote(source) + `, "<orby-workspace>", "exec"), globals(), globals())
except Exception as _script_error:
    print(str(_script_error) or _script_error.__class__.__name__, file=_sys.stderr)
    _sys.exit(1)
`
	if err := os.WriteFile(scriptPath, []byte(wrapper), 0o600); err != nil {
		return pythonRunResponse{Error: "unable to prepare Python script"}, http.StatusInternalServerError
	}

	requestReader, requestWriter, err := os.Pipe()
	if err != nil {
		return pythonRunResponse{Error: "unable to create preset bridge"}, http.StatusInternalServerError
	}
	defer requestReader.Close()
	defer requestWriter.Close()
	responseReader, responseWriter, err := os.Pipe()
	if err != nil {
		return pythonRunResponse{Error: "unable to create preset bridge"}, http.StatusInternalServerError
	}
	defer responseReader.Close()
	defer responseWriter.Close()

	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	command := exec.CommandContext(ctx, python, "-I", "-u", scriptPath)
	command.Dir = temporary
	command.Env = []string{"PATH=/usr/bin:/bin", "PYTHONIOENCODING=utf-8", "PYTHONUNBUFFERED=1"}
	command.ExtraFiles = []*os.File{requestWriter, responseReader}
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	command.WaitDelay = time.Second
	output := &limitedBuffer{limit: maxPythonOutputBytes, onWrite: onOutput}
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		return pythonRunResponse{Error: "unable to start Python: " + err.Error()}, http.StatusInternalServerError
	}
	_ = requestWriter.Close()
	_ = responseReader.Close()
	bridgeDone := make(chan struct{})
	go func() {
		defer close(bridgeDone)
		server.servePythonBridge(ctx, requestReader, responseWriter)
	}()
	waitErr := command.Wait()
	_ = requestReader.Close()
	_ = responseWriter.Close()
	<-bridgeDone
	result := pythonRunResponse{Output: output.String(), Truncated: output.Truncated()}
	if waitErr != nil {
		if strings.TrimSpace(result.Output) == "" {
			result.Error = "script exited with an error"
		}
		return result, http.StatusUnprocessableEntity
	}
	return result, http.StatusOK
}

func (server *server) servePythonBridge(ctx context.Context, reader io.Reader, writer io.Writer) {
	decoder := json.NewDecoder(io.LimitReader(reader, maxBridgeBytes))
	encoder := json.NewEncoder(writer)
	for {
		var request pythonBridgeRequest
		if err := decoder.Decode(&request); err != nil {
			return
		}
		response := server.runPresetForPython(ctx, request)
		if err := encoder.Encode(response); err != nil {
			return
		}
	}
}

func (server *server) runPresetForPython(ctx context.Context, bridge pythonBridgeRequest) pythonBridgeResponse {
	preset, ok := server.pythonPreset(bridge.Connection)
	if !ok {
		return pythonBridgeResponse{Error: fmt.Sprintf("unknown preset connection %q", bridge.Connection)}
	}
	plugin := server.plugins[preset.Tool]
	fields := maps.Clone(preset.Fields)
	connectionFields := map[string]bool{}
	for _, field := range plugin.Metadata().Fields {
		connectionFields[field.Key] = true
	}
	for key, value := range bridge.Fields {
		if !connectionFields[key] {
			fields[key] = value
		}
	}
	request := pluginapi.Request{
		Context: ctx, Query: bridge.Query, Format: strings.ToLower(strings.TrimSpace(bridge.Format)),
		ConnectionID: preset.ID, LeaseID: "python-ide", ConnectionName: preset.Name,
		Host: preset.Host, Port: preset.Port, Mode: preset.Mode, Environment: preset.Environment, Fields: fields,
	}
	if request.Format == "" {
		request.Format = plugin.Metadata().DefaultFormat
	}
	key, err := connectionKey(preset.Tool, request)
	if err != nil {
		return pythonBridgeResponse{Error: err.Error()}
	}
	connection, release, err := server.connections.Acquire(key, request.LeaseID, func() (pluginapi.Connection, error) {
		return plugin.Connect(request)
	})
	if err != nil {
		return pythonBridgeResponse{Error: err.Error()}
	}
	defer release()
	result, err := connection.Run(request)
	if err != nil {
		return pythonBridgeResponse{Error: err.Error()}
	}
	response := pythonBridgeResponse{OK: true, Tool: result.Tool, Query: result.Query, Format: result.Format, DurationMS: result.DurationMS}
	if result.HasCount {
		count := result.RowCount
		response.Count = &count
	}
	switch result.Format {
	case "raw":
		response.Data = result.Raw
	case "browse":
		response.Data = result.Browse
	case "json":
		if result.HasJSONValue {
			response.Data = result.JSONValue
		} else {
			response.Data = result.Rows
		}
	default:
		response.Data = result.Rows
	}
	return response
}

func (server *server) pythonPreset(identifier string) (presetConnection, bool) {
	identifier = strings.TrimSpace(identifier)
	for _, profile := range server.presets.Profiles {
		for _, connection := range profile.Connections {
			if connection.ID == identifier || connection.Name == identifier {
				return connection, true
			}
		}
	}
	return presetConnection{}, false
}

type limitedBuffer struct {
	mu        sync.Mutex
	data      []byte
	limit     int
	truncated bool
	onWrite   func(string)
}

func (buffer *limitedBuffer) Write(value []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	remaining := buffer.limit - len(buffer.data)
	if remaining > 0 {
		if remaining > len(value) {
			remaining = len(value)
		}
		buffer.data = append(buffer.data, value[:remaining]...)
		if buffer.onWrite != nil {
			buffer.onWrite(string(value[:remaining]))
		}
	}
	if remaining < len(value) {
		buffer.truncated = true
	}
	return len(value), nil
}

func (buffer *limitedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return string(buffer.data)
}

func (buffer *limitedBuffer) Truncated() bool {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.truncated
}

func pythonPrelude(connections []pythonConnectionView) string {
	encoded, _ := json.Marshal(connections)
	return `import json as _json
import os as _os
import resource as _resource
import shlex as _shlex
import sys as _sys
import threading as _threading
import types as _types

_resource.setrlimit(_resource.RLIMIT_AS, (256 * 1024 * 1024, 256 * 1024 * 1024))
_resource.setrlimit(_resource.RLIMIT_FSIZE, (10 * 1024 * 1024, 10 * 1024 * 1024))
_resource.setrlimit(_resource.RLIMIT_NOFILE, (64, 64))
try:
    _resource.setrlimit(_resource.RLIMIT_NPROC, (16, 16))
except (ValueError, OSError):
    pass

_CONNECTIONS = ` + string(encoded) + `
_request_pipe = _os.fdopen(3, "w", encoding="utf-8", buffering=1)
_response_pipe = _os.fdopen(4, "r", encoding="utf-8", buffering=1)
_bridge_lock = _threading.Lock()

def connections():
    return [dict(item) for item in _CONNECTIONS]

class OrbyClient:
    def __init__(self, connection):
        self.connection = connection["id"]
        self.name = connection["name"]
        self.tool = connection["tool"]
        self.environment = connection["environment"]

    def result(self, query="", format="", **fields):
        request = {"connection": self.connection, "query": query, "format": format,
                   "fields": {str(key): str(value) for key, value in fields.items()}}
        with _bridge_lock:
            _request_pipe.write(_json.dumps(request, separators=(",", ":")) + "\n")
            response = _json.loads(_response_pipe.readline())
        if not response.get("ok"):
            raise RuntimeError(response.get("error", "Orby preset request failed"))
        return response

    def run(self, query="", format="", **fields):
        return self.result(query, format, **fields).get("data")

    def execute(self, *arguments, format="raw"):
        if self.tool != "redis":
            raise TypeError("execute() is available for Redis presets; use run() for structured plugins")
        return self.run(_shlex.join(str(argument) for argument in arguments), format=format)

def client(identifier):
    for connection in _CONNECTIONS:
        if identifier in (connection["id"], connection["name"]):
            return OrbyClient(connection)
    raise KeyError("unknown preset connection: " + str(identifier))

_module = _types.ModuleType("orby")
_module.client = client
_module.connections = connections
_module.OrbyClient = OrbyClient
_sys.modules["orby"] = _module
`
}
