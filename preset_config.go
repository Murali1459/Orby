package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

type presetConfig struct {
	Profiles  []presetProfile `json:"profiles"`
	PythonIDE pythonIDEConfig `json:"-"`
}

// pythonIDEConfig is deliberately excluded from JSON marshaling. The index
// handler sends presetConfig to every browser, so including credentials here
// without json:"-" would disclose the IDE password to unauthenticated users.
type pythonIDEConfig struct {
	Enabled      bool   `json:"enabled"`
	Path         string `json:"path"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
}

type presetConfigFile struct {
	Profiles  []presetProfile `json:"profiles"`
	PythonIDE pythonIDEConfig `json:"pythonIde"`
}

type presetProfile struct {
	ID          string             `json:"id"`
	Label       string             `json:"label"`
	Connections []presetConnection `json:"connections,omitempty"`
}

type presetConnection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tool string `json:"tool"`
	// Environment is "prod" (default; read-only) or "stage" (writes
	// permitted). It is the server's own authority on this connection: the
	// browser cannot override it by forging a form field on /query.
	Environment string            `json:"environment"`
	Profile     string            `json:"profile"`
	Host        string            `json:"host"`
	Port        string            `json:"port"`
	Mode        string            `json:"mode"`
	Fields      map[string]string `json:"fields"`
	Preset      bool              `json:"preset"`
}

// connectionByID returns the preset connection with the given id, if any.
func (config presetConfig) connectionByID(id string) (presetConnection, bool) {
	for _, profile := range config.Profiles {
		for _, connection := range profile.Connections {
			if connection.ID == id {
				return connection, true
			}
		}
	}
	return presetConnection{}, false
}

func loadPresetConfig(path string, knownTools map[string]bool) (presetConfig, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return presetConfig{}, nil
		}
		return presetConfig{}, fmt.Errorf("load preset connections: %w", err)
	}
	defer file.Close()

	var fileConfig presetConfigFile
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fileConfig); err != nil {
		if errors.Is(err, io.EOF) {
			return presetConfig{}, nil
		}
		return presetConfig{}, fmt.Errorf("load preset connections: %w", err)
	}
	config := presetConfig{Profiles: fileConfig.Profiles, PythonIDE: fileConfig.PythonIDE}
	if err := validatePresetConfig(&config, knownTools); err != nil {
		return presetConfig{}, fmt.Errorf("load preset connections: %w", err)
	}
	return config, nil
}

func validatePresetConfig(config *presetConfig, knownTools map[string]bool) error {
	if err := validatePythonIDEConfig(&config.PythonIDE); err != nil {
		return err
	}
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
			connection.Environment = strings.ToLower(strings.TrimSpace(connection.Environment))
			if connection.Environment == "" {
				connection.Environment = "prod"
			}
			if connection.Environment != "prod" && connection.Environment != "stage" {
				return fmt.Errorf("connection %q has invalid environment %q", connection.Name, connection.Environment)
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

func validatePythonIDEConfig(config *pythonIDEConfig) error {
	if !config.Enabled {
		return nil
	}
	config.Path = strings.TrimSpace(config.Path)
	config.Username = strings.TrimSpace(config.Username)
	if config.Path == "" {
		config.Path = "/_orby/python"
	}
	if !strings.HasPrefix(config.Path, "/") || strings.ContainsAny(config.Path, "?#") || config.Path == "/" {
		return fmt.Errorf("python IDE path must be an absolute HTTP path")
	}
	reserved := map[string]bool{
		"/query": true, "/connect": true, "/disconnect": true,
		"/connection-status": true, "/plugin-options": true,
	}
	if reserved[config.Path] || strings.HasPrefix(config.Path, "/static/") {
		return fmt.Errorf("python IDE path %q conflicts with an existing route", config.Path)
	}
	if config.Username == "" || config.PasswordHash == "" {
		return fmt.Errorf("python IDE username and passwordHash are required when enabled")
	}
	if _, err := bcrypt.Cost([]byte(config.PasswordHash)); err != nil {
		return fmt.Errorf("python IDE passwordHash must be a valid bcrypt hash")
	}
	return nil
}
