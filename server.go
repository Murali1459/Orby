package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	pluginapi "pluginvm/plugins"
	aerospikeplugin "pluginvm/plugins/aerospike"
	redisplugin "pluginvm/plugins/redis"
)

type server struct {
	pageTemplate  *template.Template
	blockTemplate *template.Template
	staticHandler http.Handler
	plugins       map[string]pluginapi.Plugin
	presets       presetConfig
	connections   *connectionPool
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
	}, nil
}

func (server *server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	switch {
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
		return toolMetadata{Name: name, Badge: "NEW", ColorClass: "tool-future", DefaultFormat: "raw"}, false
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
	toolName, originalQuery := request.Form.Get("tool"), request.Form.Get("query")
	tool, known := server.metadataFor(toolName)
	if !known {
		server.writeQueryError(writer, tool, originalQuery, request.Form.Get("connectionName"), request.Form.Get("format"), fmt.Sprintf("unknown plugin %q", toolName))
		return
	}
	plugin := server.plugins[toolName]
	query := requestFromValues(request.Form)
	key, err := connectionKey(query.Host, query.Port, query.Mode)
	if err != nil {
		server.writeQueryError(writer, tool, originalQuery, request.Form.Get("connectionName"), request.Form.Get("format"), err.Error())
		return
	}
	connection, release, err := server.acquireConnection(key, toolName, query, plugin)
	if err != nil {
		server.writeQueryError(writer, tool, originalQuery, request.Form.Get("connectionName"), request.Form.Get("format"), err.Error())
		return
	}
	defer release()
	result, err := connection.Run(query)
	if err != nil {
		server.writeQueryError(writer, tool, originalQuery, request.Form.Get("connectionName"), request.Form.Get("format"), err.Error())
		return
	}
	server.writeQueryResult(writer, tool, result, "success")
}

func (server *server) writeQueryError(writer http.ResponseWriter, tool toolMetadata, query, profile, format, message string) {
	if format == "" {
		format = tool.DefaultFormat
	}
	server.writeQueryResult(writer, tool, queryResult{Tool: tool.Name, Query: query, Format: format, Profile: profile, Error: message}, "error")
}

func (server *server) Close() { server.connections.Close() }

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}
