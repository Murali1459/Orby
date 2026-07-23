package redis

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	redislib "github.com/redis/go-redis/v9"
	"pluginvm/plugins"
)

type redisClient interface {
	Execute([]string) (any, error)
	Ping() error
	Close() error
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

func parseRedisAddresses(hosts, defaultPort string) ([]address, error) {
	port, err := strconv.Atoi(strings.TrimSpace(defaultPort))
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("valid Redis port is required")
	}
	addresses := []address{}
	for raw := range strings.SplitSeq(hosts, ",") {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		item, err := plugins.ParseAddress(raw, port)
		if err != nil {
			return nil, fmt.Errorf("invalid Redis seed")
		}
		addresses = append(addresses, item)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("redis host is required")
	}
	return addresses, nil
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

func (client *nativeRedisClient) Execute(tokens []string) (any, error) {
	arguments := make([]any, len(tokens))
	for index, token := range tokens {
		arguments[index] = token
	}
	return client.client.Do(client.ctx, arguments...).Result()
}

func (client *nativeRedisClient) Ping() error { return client.client.Ping(client.ctx).Err() }

func (client *nativeRedisClient) Close() error { return client.client.Close() }
