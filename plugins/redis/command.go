package redis

import (
	"fmt"
	"strings"
	"unicode"

	"orby/plugins"
)

// redisCommands is the single source of truth for the read-only Redis surface.
// Its names drive both the server allow-list (redisReadCommands) and the client
// autocomplete Metadata. Keep this list read-only-only; writes are rejected.
var redisCommands = []plugins.Command{
	{Name: "BROWSE", Args: []string{"pattern", "[LIMIT n]"}},
	{Name: "GET", Args: []string{"key"}},
	{Name: "MGET", Args: []string{"key", "…"}},
	{Name: "GETRANGE", Args: []string{"key", "start", "end"}},
	{Name: "GETBIT", Args: []string{"key", "offset"}},
	{Name: "STRLEN", Args: []string{"key"}},
	{Name: "EXISTS", Args: []string{"key", "…"}},
	{Name: "TYPE", Args: []string{"key"}},
	{Name: "TTL", Args: []string{"key"}},
	{Name: "PTTL", Args: []string{"key"}},
	{Name: "DUMP", Args: []string{"key"}},
	{Name: "DBSIZE", Args: []string{}},
	{Name: "RANDOMKEY", Args: []string{}},
	{Name: "TIME", Args: []string{}},
	{Name: "INFO", Args: []string{"section"}},
	{Name: "PING", Args: []string{}},
	{Name: "ECHO", Args: []string{"message"}},
	{Name: "HGET", Args: []string{"key", "field"}},
	{Name: "HGETALL", Args: []string{"key"}},
	{Name: "HMGET", Args: []string{"key", "field", "…"}},
	{Name: "HEXISTS", Args: []string{"key", "field"}},
	{Name: "HLEN", Args: []string{"key"}},
	{Name: "HSTRLEN", Args: []string{"key", "field"}},
	{Name: "HKEYS", Args: []string{"key"}},
	{Name: "HVALS", Args: []string{"key"}},
	{Name: "HRANDFIELD", Args: []string{"key", "count"}},
	{Name: "SMEMBERS", Args: []string{"key"}},
	{Name: "SCARD", Args: []string{"key"}},
	{Name: "SISMEMBER", Args: []string{"key", "member"}},
	{Name: "SMISMEMBER", Args: []string{"key", "member", "…"}},
	{Name: "SINTER", Args: []string{"key", "…"}},
	{Name: "SINTERCARD", Args: []string{"numkeys", "key", "…"}},
	{Name: "SUNION", Args: []string{"key", "…"}},
	{Name: "SDIFF", Args: []string{"key", "…"}},
	{Name: "LRANGE", Args: []string{"key", "start", "stop"}},
	{Name: "LLEN", Args: []string{"key"}},
	{Name: "LINDEX", Args: []string{"key", "index"}},
	{Name: "SRANDMEMBER", Args: []string{"key", "count"}},
	{Name: "ZRANGE", Args: []string{"key", "start", "stop"}},
	{Name: "ZREVRANGE", Args: []string{"key", "start", "stop"}},
	{Name: "ZRANGEBYSCORE", Args: []string{"key", "min", "max"}},
	{Name: "ZREVRANGEBYSCORE", Args: []string{"key", "max", "min"}},
	{Name: "ZRANGEBYLEX", Args: []string{"key", "min", "max"}},
	{Name: "ZREVRANGEBYLEX", Args: []string{"key", "max", "min"}},
	{Name: "ZCOUNT", Args: []string{"key", "min", "max"}},
	{Name: "ZLEXCOUNT", Args: []string{"key", "min", "max"}},
	{Name: "ZCARD", Args: []string{"key"}},
	{Name: "ZSCORE", Args: []string{"key", "member"}},
	{Name: "ZRANK", Args: []string{"key", "member"}},
	{Name: "ZREVRANK", Args: []string{"key", "member"}},
	{Name: "ZINTER", Args: []string{"numkeys", "key", "…"}},
	{Name: "ZUNION", Args: []string{"numkeys", "key", "…"}},
	{Name: "ZDIFF", Args: []string{"numkeys", "key", "…"}},
	{Name: "ZINTERCARD", Args: []string{"numkeys", "key", "…"}},
	{Name: "XRANGE", Args: []string{"key", "start", "end"}},
	{Name: "XREVRANGE", Args: []string{"key", "end", "start"}},
	{Name: "XREAD", Args: []string{"COUNT", "count", "STREAMS", "key", "id"}},
	{Name: "XLEN", Args: []string{"key"}},
	{Name: "XINFO", Args: []string{"subcommand", "key"}},
	{Name: "XPENDING", Args: []string{"key", "group"}},
	{Name: "GEODIST", Args: []string{"key", "member1", "member2", "unit"}},
	{Name: "GEOPOS", Args: []string{"key", "member", "…"}},
	{Name: "GEOHASH", Args: []string{"key", "member", "…"}},
	{Name: "GEORADIUS", Args: []string{"key", "longitude", "latitude", "radius", "unit"}},
	{Name: "GEORADIUSBYMEMBER", Args: []string{"key", "member", "radius", "unit"}},
	{Name: "GEOSEARCH", Args: []string{"key", "FROMMEMBER", "member", "BYRADIUS", "radius", "unit"}},
	{Name: "PFCOUNT", Args: []string{"key", "…"}},
	{Name: "LCS", Args: []string{"key1", "key2"}},
	{Name: "OBJECT", Args: []string{"subcommand", "key"}},
}

var redisReadCommands = func() map[string]bool {
	set := make(map[string]bool, len(redisCommands))
	for _, command := range redisCommands {
		set[command.Name] = true
	}
	return set
}()

func tokenizeRedis(source string) ([]string, error) {
	tokens := []string{}
	var token strings.Builder
	var quote rune
	escaped, started := false, false
	for _, char := range source {
		if escaped {
			token.WriteRune(char)
			started, escaped = true, false
			continue
		}
		if char == '\\' {
			escaped, started = true, true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				token.WriteRune(char)
			}
			started = true
			continue
		}
		if char == '\'' || char == '"' {
			quote, started = char, true
		} else if unicode.IsSpace(char) {
			if started {
				tokens = append(tokens, token.String())
				token.Reset()
				started = false
			}
		} else {
			token.WriteRune(char)
			started = true
		}
	}
	if escaped {
		return nil, fmt.Errorf("redis command has trailing escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("redis command has unterminated quote")
	}
	if started {
		tokens = append(tokens, token.String())
	}
	return tokens, nil
}

func validateRedis(query string) ([]string, error) {
	tokens, err := tokenizeRedis(query)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("redis command cannot be empty")
	}
	command := strings.ToUpper(tokens[0])
	if !redisReadCommands[command] {
		return nil, fmt.Errorf("read-only mode rejected %q", command)
	}
	return tokens, nil
}
