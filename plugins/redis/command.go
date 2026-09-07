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
	{Name: "JSON.GET", Args: []string{"key", "[path …]"}},
	{Name: "JSON.TYPE", Args: []string{"key", "[path]"}},
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

// redisWriteCommands is autocomplete metadata only, for commands a Stage
// connection may run (see writesAllowed): it does NOT feed redisReadCommands,
// so it can never widen the Prod allow-list above. Generated once from
// redis-doc's commands.json (https://github.com/redis/redis-doc, MIT
// licensed) by filtering to commands whose command_flags contains "write";
// container-only entries (e.g. "ACL CAT") are skipped since validateRedis
// only inspects the first token. Regenerate by re-running that filter if a
// newer Redis adds write commands worth surfacing.
var redisWriteCommands = []plugins.Command{
	{Name: "APPEND", Args: []string{"key", "value"}},
	{Name: "BITFIELD", Args: []string{"key", "[GET get-block|write]", "…"}},
	{Name: "BITOP", Args: []string{"AND|OR|XOR|NOT", "destkey", "key", "…"}},
	{Name: "BLMOVE", Args: []string{"source", "destination", "LEFT|RIGHT", "LEFT|RIGHT", "timeout"}},
	{Name: "BLMPOP", Args: []string{"timeout", "numkeys", "key", "…", "LEFT|RIGHT", "[COUNT count]"}},
	{Name: "BLPOP", Args: []string{"key", "…", "timeout"}},
	{Name: "BRPOP", Args: []string{"key", "…", "timeout"}},
	{Name: "BRPOPLPUSH", Args: []string{"source", "destination", "timeout"}},
	{Name: "BZMPOP", Args: []string{"timeout", "numkeys", "key", "…", "MIN|MAX", "[COUNT count]"}},
	{Name: "BZPOPMAX", Args: []string{"key", "…", "timeout"}},
	{Name: "BZPOPMIN", Args: []string{"key", "…", "timeout"}},
	{Name: "COPY", Args: []string{"source", "destination", "[DB destination-db]", "[REPLACE]"}},
	{Name: "DECR", Args: []string{"key"}},
	{Name: "DECRBY", Args: []string{"key", "decrement"}},
	{Name: "DEL", Args: []string{"key", "…"}},
	{Name: "EXPIRE", Args: []string{"key", "seconds", "[NX|XX|GT|LT]"}},
	{Name: "EXPIREAT", Args: []string{"key", "unix-time-seconds", "[NX|XX|GT|LT]"}},
	{Name: "FLUSHALL", Args: []string{"[ASYNC|SYNC]"}},
	{Name: "FLUSHDB", Args: []string{"[ASYNC|SYNC]"}},
	{Name: "GEOADD", Args: []string{"key", "[NX|XX]", "[CH]", "longitude latitude member", "…"}},
	{Name: "GEOSEARCHSTORE", Args: []string{"destination", "source", "FROMMEMBER member|FROMLONLAT fromlonlat", "circle|box", "[ASC|DESC]", "[COUNT count ANY]", "[STOREDIST]"}},
	{Name: "GETDEL", Args: []string{"key"}},
	{Name: "GETEX", Args: []string{"key", "[EX seconds|PX milliseconds|EXAT unix-time-seconds|PXAT unix-time-milliseconds|PERSIST]"}},
	{Name: "GETSET", Args: []string{"key", "value"}},
	{Name: "HDEL", Args: []string{"key", "field", "…"}},
	{Name: "HINCRBY", Args: []string{"key", "field", "increment"}},
	{Name: "HINCRBYFLOAT", Args: []string{"key", "field", "increment"}},
	{Name: "HMSET", Args: []string{"key", "field value", "…"}},
	{Name: "HSET", Args: []string{"key", "field value", "…"}},
	{Name: "HSETNX", Args: []string{"key", "field", "value"}},
	{Name: "INCR", Args: []string{"key"}},
	{Name: "INCRBY", Args: []string{"key", "increment"}},
	{Name: "INCRBYFLOAT", Args: []string{"key", "increment"}},
	{Name: "JSON.SET", Args: []string{"key", "path", "value", "[NX|XX]"}},
	{Name: "LINSERT", Args: []string{"key", "BEFORE|AFTER", "pivot", "element"}},
	{Name: "LMOVE", Args: []string{"source", "destination", "LEFT|RIGHT", "LEFT|RIGHT"}},
	{Name: "LMPOP", Args: []string{"numkeys", "key", "…", "LEFT|RIGHT", "[COUNT count]"}},
	{Name: "LPOP", Args: []string{"key", "[count]"}},
	{Name: "LPUSH", Args: []string{"key", "element", "…"}},
	{Name: "LPUSHX", Args: []string{"key", "element", "…"}},
	{Name: "LREM", Args: []string{"key", "count", "element"}},
	{Name: "LSET", Args: []string{"key", "index", "element"}},
	{Name: "LTRIM", Args: []string{"key", "start", "stop"}},
	{Name: "MIGRATE", Args: []string{"host", "port", "key|EMPTY-STRING", "destination-db", "timeout", "[COPY]", "[REPLACE]", "[AUTH password|AUTH2 auth2]", "[KEYS key]", "…"}},
	{Name: "MOVE", Args: []string{"key", "db"}},
	{Name: "MSET", Args: []string{"key value", "…"}},
	{Name: "MSETNX", Args: []string{"key value", "…"}},
	{Name: "PERSIST", Args: []string{"key"}},
	{Name: "PEXPIRE", Args: []string{"key", "milliseconds", "[NX|XX|GT|LT]"}},
	{Name: "PEXPIREAT", Args: []string{"key", "unix-time-milliseconds", "[NX|XX|GT|LT]"}},
	{Name: "PFADD", Args: []string{"key", "[element]", "…"}},
	{Name: "PFMERGE", Args: []string{"destkey", "[sourcekey]", "…"}},
	{Name: "PSETEX", Args: []string{"key", "milliseconds", "value"}},
	{Name: "RENAME", Args: []string{"key", "newkey"}},
	{Name: "RENAMENX", Args: []string{"key", "newkey"}},
	{Name: "RESTORE", Args: []string{"key", "ttl", "serialized-value", "[REPLACE]", "[ABSTTL]", "[IDLETIME seconds]", "[FREQ frequency]"}},
	{Name: "RPOP", Args: []string{"key", "[count]"}},
	{Name: "RPOPLPUSH", Args: []string{"source", "destination"}},
	{Name: "RPUSH", Args: []string{"key", "element", "…"}},
	{Name: "RPUSHX", Args: []string{"key", "element", "…"}},
	{Name: "SADD", Args: []string{"key", "member", "…"}},
	{Name: "SDIFFSTORE", Args: []string{"destination", "key", "…"}},
	{Name: "SET", Args: []string{"key", "value", "[NX|XX]", "[GET]", "[EX seconds|PX milliseconds|EXAT unix-time-seconds|PXAT unix-time-milliseconds|KEEPTTL]"}},
	{Name: "SETBIT", Args: []string{"key", "offset", "value"}},
	{Name: "SETEX", Args: []string{"key", "seconds", "value"}},
	{Name: "SETNX", Args: []string{"key", "value"}},
	{Name: "SETRANGE", Args: []string{"key", "offset", "value"}},
	{Name: "SINTERSTORE", Args: []string{"destination", "key", "…"}},
	{Name: "SMOVE", Args: []string{"source", "destination", "member"}},
	{Name: "SORT", Args: []string{"key", "[BY pattern]", "[offset count]", "[GET pattern]", "…", "[ASC|DESC]", "[ALPHA]", "[STORE destination]"}},
	{Name: "SPOP", Args: []string{"key", "[count]"}},
	{Name: "SREM", Args: []string{"key", "member", "…"}},
	{Name: "SUNIONSTORE", Args: []string{"destination", "key", "…"}},
	{Name: "SWAPDB", Args: []string{"index1", "index2"}},
	{Name: "UNLINK", Args: []string{"key", "…"}},
	{Name: "XACK", Args: []string{"key", "group", "id", "…"}},
	{Name: "XADD", Args: []string{"key", "[NOMKSTREAM]", "[strategy operator threshold LIMIT count]", "*|id", "field value", "…"}},
	{Name: "XAUTOCLAIM", Args: []string{"key", "group", "consumer", "min-idle-time", "start", "[COUNT count]", "[JUSTID]"}},
	{Name: "XCLAIM", Args: []string{"key", "group", "consumer", "min-idle-time", "id", "…", "[IDLE ms]", "[TIME unix-time-milliseconds]", "[RETRYCOUNT count]", "[FORCE]", "[JUSTID]", "[LASTID lastid]"}},
	{Name: "XDEL", Args: []string{"key", "id", "…"}},
	{Name: "XREADGROUP", Args: []string{"group consumer", "[COUNT count]", "[BLOCK milliseconds]", "[NOACK]", "key id"}},
	{Name: "XSETID", Args: []string{"key", "last-id", "[ENTRIESADDED entries-added]", "[MAXDELETEDID max-deleted-id]"}},
	{Name: "XTRIM", Args: []string{"key", "strategy operator threshold LIMIT count"}},
	{Name: "ZADD", Args: []string{"key", "[NX|XX]", "[GT|LT]", "[CH]", "[INCR]", "score member", "…"}},
	{Name: "ZDIFFSTORE", Args: []string{"destination", "numkeys", "key", "…"}},
	{Name: "ZINCRBY", Args: []string{"key", "increment", "member"}},
	{Name: "ZINTERSTORE", Args: []string{"destination", "numkeys", "key", "…", "[WEIGHTS weight]", "…", "[SUM|MIN|MAX]"}},
	{Name: "ZMPOP", Args: []string{"numkeys", "key", "…", "MIN|MAX", "[COUNT count]"}},
	{Name: "ZPOPMAX", Args: []string{"key", "[count]"}},
	{Name: "ZPOPMIN", Args: []string{"key", "[count]"}},
	{Name: "ZRANGESTORE", Args: []string{"dst", "src", "min", "max", "[BYSCORE|BYLEX]", "[REV]", "[offset count]"}},
	{Name: "ZREM", Args: []string{"key", "member", "…"}},
	{Name: "ZREMRANGEBYLEX", Args: []string{"key", "min", "max"}},
	{Name: "ZREMRANGEBYRANK", Args: []string{"key", "start", "stop"}},
	{Name: "ZREMRANGEBYSCORE", Args: []string{"key", "min", "max"}},
	{Name: "ZUNIONSTORE", Args: []string{"destination", "numkeys", "key", "…", "[WEIGHTS weight]", "…", "[SUM|MIN|MAX]"}},
}

// allRedisCommands is the full autocomplete surface: read commands (always
// safe) plus write commands (only actually executable on a Stage connection;
// see writesAllowed). Metadata is static and connection-agnostic, so a Prod
// connection's composer still shows write suggestions — attempting one just
// surfaces the usual "read-only mode rejected" error.
var allRedisCommands = func() []plugins.Command {
	all := make([]plugins.Command, 0, len(redisCommands)+len(redisWriteCommands))
	all = append(all, redisCommands...)
	all = append(all, redisWriteCommands...)
	return all
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

// validateRedis tokenizes query and, unless allowWrites is set (a Stage
// connection), rejects any command outside the read-only allow-list.
func validateRedis(query string, allowWrites bool) ([]string, error) {
	tokens, err := tokenizeRedis(query)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, fmt.Errorf("redis command cannot be empty")
	}
	if allowWrites {
		return tokens, nil
	}
	command := strings.ToUpper(tokens[0])
	if !redisReadCommands[command] {
		return nil, fmt.Errorf("read-only mode rejected %q", command)
	}
	return tokens, nil
}
