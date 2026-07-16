package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type presetConfig struct {
	Profiles []presetProfile `json:"profiles"`
}

type presetProfile struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Connections []presetConnection `json:"connections,omitempty"`
}

type presetConnection struct {
	ID      string            `json:"id"`
	Name    string            `json:"name"`
	Tool    string            `json:"tool"`
	Profile string            `json:"profile"`
	Host    string            `json:"host"`
	Port    string            `json:"port"`
	Mode    string            `json:"mode"`
	Fields  map[string]string `json:"fields"`
	Preset  bool              `json:"preset"`
}

func loadPresetConfig(path string, knownTools map[string]bool) (presetConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		return presetConfig{}, fmt.Errorf("load preset connections: %w", err)
	}
	defer file.Close()

	var config presetConfig
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return presetConfig{}, fmt.Errorf("load preset connections: %w", err)
	}
	if err := validatePresetConfig(&config, knownTools); err != nil {
		return presetConfig{}, fmt.Errorf("load preset connections: %w", err)
	}
	return config, nil
}

func validatePresetConfig(config *presetConfig, knownTools map[string]bool) error {
	profileIDs := map[string]bool{}
	connectionIDs := map[string]bool{}
	connectionNames := map[string]bool{}
	for profileIndex := range config.Profiles {
		profile := &config.Profiles[profileIndex]
		profile.ID, profile.Label = strings.TrimSpace(profile.ID), strings.TrimSpace(profile.Label)
		if profile.ID == "" || profile.Label == "" {
			return fmt.Errorf("profile id and label are required")
		}
		if profile.ID == "saved" {
			return fmt.Errorf("profile id \"saved\" is reserved")
		}
		if profileIDs[profile.ID] {
			return fmt.Errorf("duplicate profile id %q", profile.ID)
		}
		profileIDs[profile.ID] = true

		for connectionIndex := range profile.Connections {
			connection := &profile.Connections[connectionIndex]
			connection.ID = strings.TrimSpace(connection.ID)
			connection.Name = strings.TrimSpace(connection.Name)
			connection.Tool = strings.TrimSpace(connection.Tool)
			connection.Host = strings.TrimSpace(connection.Host)
			connection.Port = strings.TrimSpace(connection.Port)
			connection.Mode = strings.TrimSpace(connection.Mode)
			if connection.ID == "" || connection.Name == "" || connection.Tool == "" || connection.Host == "" {
				return fmt.Errorf("profile %q has a connection with missing required fields", profile.ID)
			}
			if !strings.HasPrefix(connection.ID, "preset:") {
				return fmt.Errorf("connection id %q must start with \"preset:\"", connection.ID)
			}
			if connectionIDs[connection.ID] {
				return fmt.Errorf("duplicate connection id %q", connection.ID)
			}
			if connectionNames[connection.Name] {
				return fmt.Errorf("duplicate connection name %q", connection.Name)
			}
			if !knownTools[connection.Tool] {
				return fmt.Errorf("connection %q uses unknown tool %q", connection.Name, connection.Tool)
			}
			if connection.Mode != "single" && connection.Mode != "cluster" {
				return fmt.Errorf("connection %q has invalid mode %q", connection.Name, connection.Mode)
			}
			port, err := strconv.Atoi(connection.Port)
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("connection %q has invalid port %q", connection.Name, connection.Port)
			}
			if connection.Fields == nil {
				connection.Fields = map[string]string{}
			}
			connection.Profile, connection.Preset = profile.ID, true
			connectionIDs[connection.ID], connectionNames[connection.Name] = true, true
		}
	}
	return nil
}
