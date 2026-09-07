package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"sort"
	"strings"
	"time"

	pluginapi "orby/plugins"
)

type blockData struct {
	ToolName, ToolClass, ToolBadge, Query, Profile, Format, FormatLabel string
	StatusLabel, ResultStatus, Timestamp, StateJSON, VirtualKind        string
	ConnectionID, LeaseID, ConnectionName, Host, Port, Mode             string
	Environment                                                         string
	FieldsJSON                                                          string
	CountLabel                                                          string
	DurationMS                                                          int64
	IsError                                                             bool
	OutputJSON                                                          template.JS
}

type virtualOutput struct {
	Kind   string                `json:"kind"`
	Text   string                `json:"text,omitempty"`
	Heads  []string              `json:"heads,omitempty"`
	Rows   [][]string            `json:"rows,omitempty"`
	Browse *pluginapi.BrowseView `json:"browse,omitempty"`
}

func (server *server) writeQueryResult(writer http.ResponseWriter, tool toolMetadata, request queryRequest, result queryResult, status string) {
	data := blockFor(tool, request, result, status)
	var output bytes.Buffer
	if err := server.blockTemplate.ExecuteTemplate(&output, "cmd_block.html", data); err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("X-Orby-Result", status)
	writer.Write(output.Bytes())
}

func blockFor(tool toolMetadata, request queryRequest, result queryResult, status string) blockData {
	format := strings.ToLower(strings.TrimSpace(result.Format))
	if result.Error == "" && format != "json" && format != "table" && format != "raw" && format != "browse" {
		if result.IsRaw {
			format = "raw"
		} else {
			format = "json"
		}
	}
	statusLabel, resultStatus := "SUCCESS", "success"
	isError := false
	switch status {
	case "error":
		statusLabel, resultStatus, isError = "ERROR", "error", true
	case "cancelled":
		statusLabel, resultStatus = "CANCELLED", "cancelled"
	}
	state := result.State
	if state == nil {
		state = map[string]string{}
	}
	encodedState, _ := json.Marshal(state)
	fields, _ := json.Marshal(request.Fields)
	if len(fields) == 0 {
		fields = []byte("{}")
	}
	payload := virtualOutput{Kind: format}
	switch {
	case isError || status == "cancelled":
		payload.Kind, payload.Text = "error", result.Error
	case format == "browse":
		payload.Browse = result.Browse
	case format == "table":
		payload.Heads, payload.Rows = tableData(result.Rows)
	case format == "raw":
		payload.Text = result.Raw
	default:
		value := any(result.Rows)
		if result.HasJSONValue {
			value = result.JSONValue
		}
		encoded, err := pluginapi.MarshalJSON(value, "  ")
		if err != nil {
			encoded = []byte(err.Error())
		}
		payload.Text = string(encoded)
	}
	outputJSON, _ := json.Marshal(payload)
	return blockData{ToolName: tool.Name, ToolClass: tool.ColorClass, ToolBadge: tool.Badge, Query: result.Query, Profile: pluginapi.ProfileName(result.Profile), Format: format, FormatLabel: strings.ToUpper(format), StatusLabel: statusLabel, ResultStatus: resultStatus, Timestamp: time.Now().Format("15:04:05"), StateJSON: string(encodedState), ConnectionID: request.ConnectionID, LeaseID: request.LeaseID, ConnectionName: request.ConnectionName, Host: request.Host, Port: request.Port, Mode: request.Mode, Environment: normalizeEnvironment(request.Environment), FieldsJSON: string(fields), VirtualKind: payload.Kind, CountLabel: resultCountLabel(result, isError), DurationMS: result.DurationMS, IsError: isError, OutputJSON: template.JS(outputJSON)}
}

func resultCountLabel(result queryResult, isError bool) string {
	if isError || !result.HasCount {
		return ""
	}
	unit := result.CountUnit
	if unit == "" {
		unit = "record"
	}
	if result.RowCount != 1 {
		unit += "s"
	}
	return fmt.Sprintf("%d %s", result.RowCount, unit)
}

func tableData(rows []map[string]any) ([]string, [][]string) {
	heads := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		for key := range row {
			if !seen[key] {
				seen[key] = true
				heads = append(heads, key)
			}
		}
	}
	sort.Strings(heads)
	values := make([][]string, len(rows))
	for index, row := range rows {
		for _, head := range heads {
			value := ""
			if item, ok := row[head]; ok {
				value = tableValue(item)
			}
			values[index] = append(values[index], value)
		}
	}
	return heads, values
}

func tableValue(value any) string {
	switch value.(type) {
	case map[string]any, []any:
		if encoded, err := pluginapi.MarshalJSON(value, ""); err == nil {
			return string(encoded)
		}
	}
	return fmt.Sprint(value)
}
