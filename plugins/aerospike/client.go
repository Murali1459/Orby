package aerospike

import (
	"time"

	as "github.com/aerospike/aerospike-client-go/v8"
	"pluginvm/plugins"
)

type clientAPI interface {
	Get(namespace, set, primaryKey string, filter *as.Expression) (map[string]any, error)
	Scan(namespace, set string, filter *as.Expression, limit int) ([]map[string]any, error)
	Info(command string) ([]string, error)
	Close()
}

var clientFactory = newNativeClient

type nativeClient struct{ client *as.Client }

func newNativeClient(seeds []plugins.Address) (clientAPI, error) {
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

func (client *nativeClient) Get(namespace, set, primaryKey string, filter *as.Expression) (map[string]any, error) {
	key, err := as.NewKey(namespace, set, primaryKey)
	if err != nil {
		return nil, err
	}
	policy := as.NewPolicy()
	policy.TotalTimeout, policy.FilterExpression = 10*time.Second, filter
	record, err := client.client.Get(policy, key)
	if err != nil {
		return nil, err
	}
	return nativeRecord(record), nil
}

func (client *nativeClient) Scan(namespace, set string, filter *as.Expression, limit int) ([]map[string]any, error) {
	policy := as.NewScanPolicy()
	policy.TotalTimeout, policy.FilterExpression = 10*time.Second, filter
	recordset, err := client.client.ScanAll(policy, namespace, set)
	if err != nil {
		return nil, err
	}
	defer recordset.Close()
	rows := []map[string]any{}
	for result := range recordset.Results() {
		if result.Err != nil {
			return nil, result.Err
		}
		rows = append(rows, nativeRecord(result.Record))
		if limit > 0 && len(rows) >= limit {
			recordset.Close()
			break
		}
	}
	return rows, nil
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

func nativeRecord(record *as.Record) map[string]any {
	if record == nil {
		return nil
	}
	var key any
	namespace, set := "", ""
	if record.Key != nil {
		namespace, set = record.Key.Namespace(), record.Key.SetName()
		if record.Key.Value() != nil {
			key = record.Key.Value().GetObject()
		}
	}
	bins := map[string]any{}
	for name, value := range record.Bins {
		bins[name] = value
	}
	return map[string]any{"key": key, "namespace": namespace, "set": set, "generation": record.Generation, "expiration": record.Expiration, "bins": bins}
}

func (client *nativeClient) Close() { client.client.Close() }
