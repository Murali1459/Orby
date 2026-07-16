package plugins

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type Address struct {
	Host string
	Port int
}

type Field struct {
	Label       string `json:"label"`
	Key         string `json:"key"`
	Default     string `json:"default"`
	Placeholder string `json:"placeholder"`
	InputType   string `json:"inputType"`
}

type ComposerElement struct {
	Kind        string `json:"kind"`
	Text        string `json:"text,omitempty"`
	Name        string `json:"name,omitempty"`
	Default     string `json:"default,omitempty"`
	InputType   string `json:"inputType,omitempty"`
	Placement   string `json:"placement,omitempty"`
	Icon        string `json:"icon,omitempty"`
	HideNarrow  bool   `json:"hideNarrow,omitempty"`
	Placeholder string `json:"placeholder,omitempty"`
	Options     string `json:"options,omitempty"`
	DependsOn   string `json:"dependsOn,omitempty"`
	Action      string `json:"action,omitempty"`
	Grow        bool   `json:"grow,omitempty"`
}

type ExpressionType struct {
	Value     string           `json:"value"`
	Label     string           `json:"label"`
	InputType string           `json:"inputType"`
	Operators []FilterOperator `json:"operators"`
}

type ExpressionEditor struct {
	BinOptions string           `json:"binOptions"`
	Types      []ExpressionType `json:"types"`
}

type Composer struct {
	Elements   []ComposerElement `json:"elements"`
	Expression *ExpressionEditor `json:"expression,omitempty"`
}

type FilterOperator struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Metadata struct {
	Name          string   `json:"name"`
	Label         string   `json:"label"`
	Badge         string   `json:"badge"`
	ColorClass    string   `json:"colorClass"`
	Icon          string   `json:"icon"`
	DefaultFormat string   `json:"defaultFormat"`
	Formats       []string `json:"formats"`
	Fields        []Field  `json:"fields"`
	Composer      Composer `json:"composer"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Request struct {
	Query          string
	Format         string
	ConnectionID   string
	LeaseID        string
	ConnectionName string
	Host           string
	Port           string
	Mode           string
	Fields         map[string]string
}

type Result struct {
	Tool         string
	Query        string
	Format       string
	Profile      string
	Rows         []map[string]any
	JSONValue    any
	HasJSONValue bool
	Raw          string
	IsRaw        bool
	Error        string
	RowCount     int
	HasCount     bool
	DurationMS   int64
	Succeeded    bool
	State        map[string]string
}

type Connection interface {
	Run(Request) (Result, error)
	Close() error
}

type ConnectionOptionProvider interface {
	Options(Request, string) ([]Option, error)
}

type Plugin interface {
	Metadata() Metadata
	Connect(Request) (Connection, error)
}

func ParseAddress(raw string, defaultPort int) (Address, error) {
	host, textPort := raw, strconv.Itoa(defaultPort)
	if strings.HasPrefix(raw, "[") {
		closing := strings.IndexByte(raw, ']')
		if closing < 0 {
			return Address{}, fmt.Errorf("invalid IPv6 seed")
		}
		host, raw = raw[1:closing], raw[closing+1:]
		if raw != "" {
			if !strings.HasPrefix(raw, ":") {
				return Address{}, fmt.Errorf("invalid seed")
			}
			textPort = raw[1:]
		}
	} else if strings.Count(raw, ":") == 1 {
		host, textPort, _ = strings.Cut(raw, ":")
	}
	port, err := strconv.Atoi(textPort)
	if err != nil || host == "" || port < 1 || port > 65535 {
		return Address{}, fmt.Errorf("valid cluster seed ports are required")
	}
	return Address{Host: strings.Trim(host, "[]"), Port: port}, nil
}

func ProfileName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "manual"
}

func MarshalJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte("\n")), nil
}
