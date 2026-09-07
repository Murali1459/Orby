package redis

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

type redisClient interface {
	Execute(ctx context.Context, tokens []string) (any, error)
	Ping() error
	Close() error
	Scan(ctx context.Context, cursor string, pattern string, count int64) (keys []string, nextCursor string, err error)
	ScanCluster(ctx context.Context, pattern string, count int64, limit int) (keys []string, limited bool, err error)
	Types(ctx context.Context, keys []string) ([]string, error) // pipelined TYPE, one result per input key, same order
	TTLs(ctx context.Context, keys []string) ([]int64, error)   // pipelined TTL in seconds, one result per input key, same order
}

var redisClientFactory = newNativeRedisClient

func redisDB(fields map[string]string) (int, error) {
	text := strings.TrimSpace(fields["dbIndex"])
	if text == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("redis DB index must be a non-negative integer")
	}
	return value, nil
}

type nativeRedisClient struct {
	client redislib.UniversalClient
	ctx    context.Context
}

func newNativeRedisClient(cluster bool, addresses []address, db int) (redisClient, error) {
	items := make([]string, len(addresses))
	for index, item := range addresses {
		items[index] = net.JoinHostPort(item.Host, strconv.Itoa(item.Port))
	}
	client := redislib.NewUniversalClient(&redislib.UniversalOptions{Addrs: items, DB: db, IsClusterMode: cluster, DialTimeout: 2 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second})
	return &nativeRedisClient{client: client, ctx: context.Background()}, nil
}

func (client *nativeRedisClient) Execute(ctx context.Context, tokens []string) (any, error) {
	arguments := make([]any, len(tokens))
	for index, token := range tokens {
		arguments[index] = token
	}
	return client.client.Do(ctx, arguments...).Result()
}

func (client *nativeRedisClient) Ping() error { return client.client.Ping(client.ctx).Err() }

func (client *nativeRedisClient) Close() error { return client.client.Close() }

func (client *nativeRedisClient) Scan(ctx context.Context, cursor string, pattern string, count int64) ([]string, string, error) {
	start, err := strconv.ParseUint(cursor, 10, 64)
	if err != nil {
		return nil, "", fmt.Errorf("invalid SCAN cursor %q", cursor)
	}
	keys, next, err := client.client.Scan(ctx, start, pattern, count).Result()
	if err != nil {
		return nil, "", err
	}
	return keys, strconv.FormatUint(next, 10), nil
}

// ScanCluster walks every master node with its own SCAN cursor because
// ClusterClient.Scan itself routes to a single random node. Keys are
// deduplicated by name across nodes (resharding can surface duplicates);
// limited reports whether the cap was hit before every node finished.
func (client *nativeRedisClient) ScanCluster(ctx context.Context, pattern string, count int64, limit int) ([]string, bool, error) {
	cluster, ok := client.client.(*redislib.ClusterClient)
	if !ok {
		return nil, false, fmt.Errorf("cluster-wide SCAN requires a cluster connection")
	}
	type nodeCursor struct {
		client *redislib.Client
		cursor uint64
		done   bool
	}
	var nodes []*nodeCursor
	lock := sync.Mutex{}
	if err := cluster.ForEachMaster(ctx, func(ctx context.Context, node *redislib.Client) error {
		lock.Lock()
		nodes = append(nodes, &nodeCursor{client: node})
		lock.Unlock()
		return nil
	}); err != nil {
		return nil, false, err
	}
	if len(nodes) == 0 {
		return nil, false, fmt.Errorf("cluster has no master nodes")
	}
	seen := map[string]struct{}{}
	keys := make([]string, 0, limit)
	active := len(nodes)
	for active > 0 {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		for _, node := range nodes {
			if node.done {
				continue
			}
			page, next, scanErr := node.client.Scan(ctx, node.cursor, pattern, count).Result()
			if scanErr != nil {
				return nil, false, scanErr
			}
			for _, key := range page {
				if _, exists := seen[key]; exists {
					continue // a key may appear on more than one node during resharding
				}
				seen[key] = struct{}{}
				keys = append(keys, key)
				if len(keys) >= limit {
					return keys, true, nil
				}
			}
			if next == 0 || next == node.cursor {
				node.done = true // cursor exhausted, or a cursor that made no progress
				active--
			} else {
				node.cursor = next
			}
		}
	}
	return keys, false, nil
}

func (client *nativeRedisClient) Types(ctx context.Context, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := client.client.Pipeline()
	commands := make([]*redislib.StatusCmd, len(keys))
	for index, key := range keys {
		commands[index] = pipe.Type(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	types := make([]string, len(keys))
	for index, command := range commands {
		types[index] = command.Val()
	}
	return types, nil
}

func (client *nativeRedisClient) TTLs(ctx context.Context, keys []string) ([]int64, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	pipe := client.client.Pipeline()
	commands := make([]*redislib.DurationCmd, len(keys))
	for index, key := range keys {
		commands[index] = pipe.TTL(ctx, key)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, err
	}
	ttls := make([]int64, len(keys))
	for index, command := range commands {
		ttls[index] = int64(command.Val().Seconds())
	}
	return ttls, nil
}
