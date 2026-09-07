package main

import (
	"encoding/json"

	pluginapi "orby/plugins"
)

type pythonPortableHost struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type pythonPortableConnection struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Tool        string               `json:"tool"`
	Environment string               `json:"environment"`
	Mode        string               `json:"mode"`
	Hosts       []pythonPortableHost `json:"hosts"`
	Fields      map[string]string    `json:"fields"`
}

func (server *server) pythonPortableConnections() string {
	connections := make([]pythonPortableConnection, 0)
	for _, profile := range server.presets.Profiles {
		for _, connection := range profile.Connections {
			addresses, err := pluginapi.ParseSeeds(connection.Host, connection.Port, "connection")
			if err != nil {
				continue
			}
			hosts := make([]pythonPortableHost, len(addresses))
			for index, address := range addresses {
				hosts[index] = pythonPortableHost{Host: address.Host, Port: address.Port}
			}
			connections = append(connections, pythonPortableConnection{
				ID: connection.ID, Name: connection.Name, Tool: connection.Tool,
				Environment: connection.Environment, Mode: connection.Mode,
				Hosts: hosts, Fields: connection.Fields,
			})
		}
	}
	encoded, _ := json.Marshal(connections)
	return string(encoded)
}
