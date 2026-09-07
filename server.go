package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	pluginapi "orby/plugins"
	aerospikeplugin "orby/plugins/aerospike"
	redisplugin "orby/plugins/redis"
)

type server struct {
	pageTemplate   *template.Template
	blockTemplate  *template.Template
	staticHandler  http.Handler
	plugins        map[string]pluginapi.Plugin
	presets        presetConfig
	connections    *connectionPool
	pythonRuns     chan struct{}
	pythonSessions pythonSessionStore
}

func newServer() (*server, error) {
	pageTemplate, blockTemplate, err := loadTemplates()
	if err != nil {
		return nil, err
	}
	registry := map[string]pluginapi.Plugin{}
	for _, plugin := range []pluginapi.Plugin{aerospikeplugin.New(), redisplugin.New()} {
		registry[plugin.Metadata().Name] = plugin
	}
	knownTools := make(map[string]bool, len(registry))
	for name := range registry {
		knownTools[name] = true
	}
	connectionsPath := strings.TrimSpace(os.Getenv("CONNECTIONS_FILE"))
	if connectionsPath == "" {
		connectionsPath = "connections.json"
	}
	presets, err := loadPresetConfig(connectionsPath, knownTools)
	if err != nil {
		return nil, err
	}
	return &server{
		pageTemplate: pageTemplate, blockTemplate: blockTemplate,
		staticHandler: staticHandler,
		plugins:       registry, presets: presets, connections: newConnectionPool(10 * time.Minute),
		pythonRuns: make(chan struct{}, 2),
	}, nil
}

func (server *server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if !requestOriginAllowed(request) {
		http.Error(writer, "cross-site request rejected", http.StatusForbidden)
		return
	}
	switch {
	case server.presets.PythonIDE.Enabled && (request.URL.Path == server.presets.PythonIDE.Path || request.URL.Path == server.presets.PythonIDE.Path+"/login" || request.URL.Path == server.presets.PythonIDE.Path+"/logout"):
		server.pythonIDE(writer, request)
	case request.URL.Path == "/":
		server.index(writer, request)
	case request.URL.Path == "/query":
		server.query(writer, request)
	case request.URL.Path == "/plugin-options":
		server.pluginOptions(writer, request)
	case request.URL.Path == "/connect":
		server.connect(writer, request)
	case request.URL.Path == "/disconnect":
		server.disconnect(writer, request)
	case request.URL.Path == "/connection-status":
		server.connectionStatus(writer, request)
	case strings.HasPrefix(request.URL.Path, "/static/"):
		writer.Header().Set("Cache-Control", "no-cache")
		server.staticHandler.ServeHTTP(writer, request)
	default:
		http.NotFound(writer, request)
	}
}

func requireMethod(writer http.ResponseWriter, request *http.Request, method string) bool {
	if request.Method == method {
		return true
	}
	writer.Header().Set("Allow", method)
	http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func requestOriginAllowed(request *http.Request) bool {
	if request.Method != http.MethodPost {
		return true
	}
	return !strings.EqualFold(request.Header.Get("Sec-Fetch-Site"), "cross-site")
}

func (server *server) index(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodGet) {
		return
	}
	encoded, _ := json.Marshal(server.toolList())
	presets, _ := json.Marshal(server.presets)
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		ToolsJSON   template.JS
		PresetsJSON template.JS
	}{template.JS(encoded), template.JS(presets)}
	if err := server.pageTemplate.ExecuteTemplate(writer, "page.html", data); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
	}
}

func (server *server) toolList() []toolMetadata {
	names := make([]string, 0, len(server.plugins))
	for name := range server.plugins {
		names = append(names, name)
	}
	sort.Strings(names)
	tools := make([]toolMetadata, len(names))
	for index, name := range names {
		tools[index] = server.plugins[name].Metadata()
	}
	return tools
}

func (server *server) metadataFor(name string) (toolMetadata, bool) {
	plugin, ok := server.plugins[name]
	if !ok {
		return toolMetadata{Name: name, DefaultFormat: "raw"}, false
	}
	return plugin.Metadata(), true
}

func (server *server) query(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	if err := request.ParseForm(); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	toolName := request.Form.Get("tool")
	tool, known := server.metadataFor(toolName)
	query := requestFromValues(request.Form)
	query.Environment = server.resolveEnvironment(query)
	if !known {
		server.writeQueryError(writer, tool, query, fmt.Sprintf("unknown plugin %q", toolName))
		return
	}
	plugin := server.plugins[toolName]
	key, err := connectionKey(toolName, query)
	if err != nil {
		server.writeQueryError(writer, tool, query, err.Error())
		return
	}
	connection, release, err := server.acquireConnection(key, query, plugin)
	if err != nil {
		server.writeQueryError(writer, tool, query, err.Error())
		return
	}
	defer release()
	// A browser abort cancels request.Context(), which is the Cancel control: the
	// UI aborts the query request, and net/http cancels the plugin's Run context.
	ctx, cancel := context.WithCancel(request.Context())
	query.Context = ctx
	defer cancel()
	result, err := connection.Run(query)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			server.writeCancelled(writer, tool, query, "Query cancelled")
			return
		}
		server.writeQueryError(writer, tool, query, err.Error())
		return
	}
	server.writeQueryResult(writer, tool, query, result, "success")
}

// resolveEnvironment is the sole authority on whether writes are allowed: for
// a preset connection it returns connections.json's own declared value,
// ignoring whatever the browser submitted, so a forged "environment=stage"
// form field can never escalate a preset the operator configured as prod. An
// ad-hoc (non-preset) connection has no server-side record to defer to, so
// the client's own input is trusted (normalized, defaulting to "prod").
func (server *server) resolveEnvironment(query queryRequest) string {
	if isPresetConnectionID(query.ConnectionID) {
		if preset, ok := server.presets.connectionByID(query.ConnectionID); ok {
			return preset.Environment
		}
		return "prod"
	}
	return normalizeEnvironment(query.Environment)
}

func (server *server) writeCancelled(writer http.ResponseWriter, tool toolMetadata, request queryRequest, message string) {
	server.writeQueryResult(writer, tool, request, queryResult{
		Tool: tool.Name, Query: request.Query, Format: request.Format, Profile: request.ConnectionName, Error: message,
	}, "cancelled")
}

func (server *server) writeQueryError(writer http.ResponseWriter, tool toolMetadata, request queryRequest, message string) {
	if request.Format == "" {
		request.Format = tool.DefaultFormat
	}
	server.writeQueryResult(writer, tool, request, queryResult{Tool: tool.Name, Query: request.Query, Format: request.Format, Profile: request.ConnectionName, Error: message}, "error")
}

func (server *server) Close() { server.connections.Close() }

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}
