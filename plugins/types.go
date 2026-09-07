package plugins

import (
	"bytes"
	"context"
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
	Decorative  bool   `json:"decorative,omitempty"`
}

type ExpressionType struct {
	Value     string           `json:"value"`
	Label     string           `json:"label"`
	InputType string           `json:"inputType"`
	Operators []FilterOperator `json:"operators"`
}

type ExpressionEditor struct {
	Types []ExpressionType `json:"types"`
}

type Composer struct {
	Elements   []ComposerElement `json:"elements"`
	Expression *ExpressionEditor `json:"expression,omitempty"`
}

type FilterOperator struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Command is one autocomplete entry a plugin offers: a keyword and its arg placeholder hints.
type Command struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

type Metadata struct {
	Name          string    `json:"name"`
	Label         string    `json:"label"`
	Badge         string    `json:"badge"`
	ColorClass    string    `json:"colorClass"`
	Icon          string    `json:"icon"`
	DefaultFormat string    `json:"defaultFormat"`
	Formats       []string  `json:"formats"`
	Fields        []Field   `json:"fields"`
	Composer      Composer  `json:"composer"`
	Commands      []Command `json:"commands,omitempty"`
}

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type Request struct {
	// Context, when non-nil, is canceled when the client aborts the query
	// (cancel button or dropped connection). Plugins should use it to stop
	// long-running operations; nil means "no cancellation requested".
	Context        context.Context
	Query          string
	Format         string
	ConnectionID   string
	LeaseID        string
	ConnectionName string
	Host           string
	Port           string
	Mode           string
	// Environment is "prod" or "stage", resolved authoritatively by the
	// server before a plugin ever sees it: for a preset connection it comes
	// from the server's own connections.json (a client cannot override it by
	// forging the form field), and only an ad-hoc connection's own client
	// input is trusted. Plugins should treat anything other than "stage" as
	// "prod" and reject writes accordingly.
	Environment string
	Fields      map[string]string
}

type BrowseKey struct {
	Name string `json:"name"`
	Type string `json:"type"`
	TTL  int64  `json:"ttl,omitempty"` // seconds; omitted when persistent (-1) or vanished (-2)
}

type BrowseGroup struct {
	Prefix string      `json:"prefix"`
	Keys   []BrowseKey `json:"keys"`
	Total  int         `json:"total"` // scanned keys in this group; may exceed len(Keys)
}

type BrowseView struct {
	Pattern string        `json:"pattern"`
	Groups  []BrowseGroup `json:"groups"`
	Scanned int           `json:"scanned"`
	Limited bool          `json:"limited"`
	Cluster bool          `json:"cluster,omitempty"`
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
	CountUnit    string
	DurationMS   int64
	Succeeded    bool
	State        map[string]string
	Browse       *BrowseView `json:"browse,omitempty"`
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

func ParseSeeds(hosts, defaultPort, what string) ([]Address, error) {
	port, err := strconv.Atoi(strings.TrimSpace(defaultPort))
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("valid %s port is required", what)
	}
	seeds := []Address{}
	for raw := range strings.SplitSeq(hosts, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		seed, err := ParseAddress(raw, port)
		if err != nil {
			return nil, fmt.Errorf("invalid %s seed", what)
		}
		seeds = append(seeds, seed)
	}
	if len(seeds) == 0 {
		return nil, fmt.Errorf("%s host is required", what)
	}
	return seeds, nil
}

func WalkJSON(value any, leaf func(any) any) any {
	switch value := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[key] = WalkJSON(item, leaf)
		}
		return out
	case map[any]any:
		// Aerospike's binary unpacker returns CDT map bins as map[any]any; encoding/json never does.
		out := make(map[string]any, len(value))
		for key, item := range value {
			out[fmt.Sprint(key)] = WalkJSON(item, leaf)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for index, item := range value {
			out[index] = WalkJSON(item, leaf)
		}
		return out
	default:
		if leaf == nil {
			return value
		}
		return leaf(value)
	}
}

func ProfileName(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return "manual"
}

func MarshalJSON(value any, indent string) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if indent != "" {
		encoder.SetIndent("", indent)
	}
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(output.Bytes(), []byte("\n")), nil
}
