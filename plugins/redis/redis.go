package redis

import (
	"fmt"
	"strings"
	"time"

	"pluginvm/plugins"
)

type address = plugins.Address
type queryRequest = plugins.Request
type queryResult = plugins.Result

type Plugin struct{}

func New() plugins.Plugin { return Plugin{} }

func (Plugin) Metadata() plugins.Metadata {
	return plugins.Metadata{
		Name: "redis", Label: "Redis", Badge: "REDIS", ColorClass: "tool-redis", Icon: "/static/icons/redis.svg",
		DefaultFormat: "raw", Formats: []string{"json", "table", "raw"},
		Composer: plugins.Composer{Elements: []plugins.ComposerElement{
			{Kind: "literal", Text: "❯"},
			{Kind: "input", Name: "query", Placeholder: "Enter Redis command", Grow: true},
		}},
		Fields: []plugins.Field{{Label: "DB Index", Key: "dbIndex", Default: "0", InputType: "number"}},
	}
}

func (Plugin) Connect(request plugins.Request) (plugins.Connection, error) {
	cluster := strings.EqualFold(strings.TrimSpace(request.Mode), "cluster")
	db, err := redisDB(request.Fields)
	if err != nil {
		return nil, err
	}
	if cluster && db != 0 {
		return nil, fmt.Errorf("redis cluster supports DB index 0 only")
	}
	addresses, err := parseRedisAddresses(request.Host, request.Port)
	if err != nil {
		return nil, err
	}
	client, err := redisClientFactory(cluster, addresses, db)
	if err != nil {
		return nil, err
	}
	if err := client.Ping(); err != nil {
		_ = client.Close()
		return nil, err
	}
	return &connection{client: client}, nil
}

type connection struct{ client redisClient }

func (connection *connection) Run(request plugins.Request) (plugins.Result, error) {
	return runRedisWithClient(request, connection.client)
}

func (connection *connection) Close() error { return connection.client.Close() }

func runRedisWithClient(request queryRequest, client redisClient) (queryResult, error) {
	started := time.Now()
	tokens, err := validateRedis(request.Query)
	if err != nil {
		return queryResult{}, err
	}
	format := request.Format
	if format == "" {
		format = "raw"
	}
	if format != "json" && format != "table" && format != "raw" {
		return queryResult{}, fmt.Errorf("unsupported Redis format %q", format)
	}
	value, err := client.Execute(tokens)
	if err != nil {
		return queryResult{}, err
	}
	value = normalizeRedis(value)
	jsonValue := redisJSON(value)
	result := queryResult{Tool: "redis", Query: request.Query, Format: format, Profile: plugins.ProfileName(request.ConnectionName), DurationMS: time.Since(started).Milliseconds(), State: map[string]string{"query": request.Query}, Succeeded: true}
	result.RowCount, result.HasCount = redisCollectionCount(jsonValue)
	switch format {
	case "table":
		result.Rows = redisRows(jsonValue)
	case "json":
		result.JSONValue, result.HasJSONValue = jsonValue, true
	case "raw":
		result.Raw, result.IsRaw = redisRaw(value), true
	}
	return result, nil
}
