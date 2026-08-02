package aerospike

import (
	"time"

	as "github.com/aerospike/aerospike-client-go/v8"
	"orby/plugins"
)

type nativeClient struct{ client *as.Client }

func newNativeClient(seeds []plugins.Address) (*nativeClient, error) {
	hosts := make([]*as.Host, len(seeds))
	for index, seed := range seeds {
		hosts[index] = as.NewHost(seed.Host, seed.Port)
	}
	policy := as.NewClientPolicy()
	policy.Timeout = 2 * time.Second
	client, err := as.NewClientWithPolicyAndHost(policy, hosts...)
	if err != nil {
		return nil, err
	}
	return &nativeClient{client: client}, nil
}

func (client *nativeClient) Get(namespace, set, primaryKey string, filter *as.Expression) (*as.Record, error) {
	key, err := as.NewKey(namespace, set, primaryKey)
	if err != nil {
		return nil, err
	}
	policy := as.NewPolicy()
	policy.TotalTimeout, policy.FilterExpression = 10*time.Second, filter
	return client.client.Get(policy, key)
}

func (client *nativeClient) Scan(namespace, set string, filter *as.Expression, limit int) (*as.Recordset, error) {
	policy := as.NewScanPolicy()
	if limit > 0 {
		policy.MaxRecords = int64(limit)
	}
	policy.TotalTimeout, policy.FilterExpression = 10*time.Second, filter
	return client.client.ScanAll(policy, namespace, set)
}

func (client *nativeClient) Info(command string) ([]string, error) {
	values := make([]string, 0, len(client.client.GetNodes()))
	for _, node := range client.client.GetNodes() {
		response, err := node.RequestInfo(as.NewInfoPolicy(), command)
		if err != nil {
			return nil, err
		}
		values = append(values, response[command])
	}
	return values, nil
}

func (client *nativeClient) IsConnected() bool { return client.client.IsConnected() }

func (client *nativeClient) Close() { client.client.Close() }
