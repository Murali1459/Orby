package redis

import (
	"fmt"
	"strings"
	"unicode"
)

var redisReadCommands = stringSet("GET MGET GETRANGE STRLEN EXISTS TYPE TTL PTTL SCAN KEYS DBSIZE INFO PING ECHO HGET HGETALL HMGET HEXISTS HLEN HKEYS HVALS LRANGE LLEN LINDEX SMEMBERS SCARD SISMEMBER SRANDMEMBER ZRANGE ZREVRANGE ZCARD ZSCORE XRANGE XREVRANGE XLEN SSCAN HSCAN ZSCAN")

func stringSet(values string) map[string]bool {
	set := map[string]bool{}
	for _, value := range strings.Fields(values) {
		set[value] = true
	}
	return set
}

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
	if !redisReadCommands[strings.ToUpper(tokens[0])] {
		return nil, fmt.Errorf("read-only mode rejected %q", strings.ToUpper(tokens[0]))
	}
	return tokens, nil
}
