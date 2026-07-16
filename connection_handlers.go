package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	pluginapi "pluginvm/plugins"
)

func (server *server) connectionStatus(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodGet) {
		return
	}
	session := strings.TrimSpace(request.URL.Query().Get("session"))
	if session == "" {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"active": []string{}, "message": "session is required"})
		return
	}
	prefix := session + ":"
	active := []string{}
	for _, lease := range server.connections.ActiveLeases() {
		if strings.HasPrefix(lease, prefix) {
			active = append(active, strings.TrimPrefix(lease, prefix))
		}
	}
	writeJSON(writer, http.StatusOK, map[string]any{"active": active})
}

func (server *server) pluginOptions(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodGet) {
		return
	}
	toolName, resource := request.URL.Query().Get("tool"), request.URL.Query().Get("resource")
	plugin, ok := server.plugins[toolName]
	if !ok {
		http.Error(writer, fmt.Sprintf("unknown plugin %q", toolName), http.StatusBadRequest)
		return
	}
	values := request.URL.Query()
	query := requestFromValues(values)
	key, err := connectionKey(query.Host, query.Port, query.Mode)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	connection, release, err := server.acquireConnection(key, toolName, query, plugin)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errConnectionNotConnected) {
			status = http.StatusConflict
		}
		http.Error(writer, err.Error(), status)
		return
	}
	defer release()
	provider, ok := connection.(pluginapi.ConnectionOptionProvider)
	if !ok {
		http.Error(writer, fmt.Sprintf("plugin %q has no live options", toolName), http.StatusBadRequest)
		return
	}
	options, err := provider.Options(query, resource)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(writer).Encode(struct {
		Options []pluginapi.Option `json:"options"`
	}{Options: options})
}

func (server *server) acquireConnection(key, toolName string, request queryRequest, plugin pluginapi.Plugin) (pluginapi.Connection, func(), error) {
	lease := requestLease(request)
	if strings.HasPrefix(request.ConnectionID, "preset:") {
		return server.connections.AcquireExisting(key, toolName, lease)
	}
	return server.connections.Acquire(key, toolName, lease, func() (pluginapi.Connection, error) {
		return plugin.Connect(request)
	})
}

func (server *server) connect(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	if err := request.ParseForm(); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"reachable": false, "message": err.Error()})
		return
	}
	toolName := request.Form.Get("tool")
	plugin, ok := server.plugins[toolName]
	if !ok {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"reachable": false, "message": fmt.Sprintf("unknown plugin %q", toolName)})
		return
	}
	query := requestFromValues(request.Form)
	key, err := connectionKey(query.Host, query.Port, query.Mode)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"reachable": false, "message": err.Error()})
		return
	}
	if err := server.connections.Connect(key, toolName, requestLease(query), func() (pluginapi.Connection, error) {
		return plugin.Connect(query)
	}); err != nil {
		writeJSON(writer, http.StatusBadGateway, map[string]any{"reachable": false, "message": err.Error()})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"reachable": true, "message": key + " connected"})
}

func (server *server) disconnect(writer http.ResponseWriter, request *http.Request) {
	if !requireMethod(writer, request, http.MethodPost) {
		return
	}
	if err := request.ParseForm(); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"disconnected": false, "message": err.Error()})
		return
	}
	key, err := connectionKey(request.Form.Get("host"), request.Form.Get("port"), request.Form.Get("mode"))
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]any{"disconnected": false, "message": err.Error()})
		return
	}
	server.connections.Disconnect(key, requestLease(requestFromValues(request.Form)))
	writeJSON(writer, http.StatusOK, map[string]any{"disconnected": true, "message": key + " disconnected"})
}
