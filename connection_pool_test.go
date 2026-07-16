package main

import (
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pluginapi "pluginvm/plugins"
)

func TestConnectionPoolReportsOnlyCurrentLeases(t *testing.T) {
	pool := newConnectionPool(time.Minute)
	defer pool.Close()
	connection := &fakePooledConnection{}
	factory := func() (pluginapi.Connection, error) { return connection, nil }
	if err := pool.Connect("redis.internal:6379", "redis", "second", factory); err != nil {
		t.Fatal(err)
	}
	if err := pool.Connect("redis.internal:6379", "redis", "first", factory); err != nil {
		t.Fatal(err)
	}
	if got := pool.ActiveLeases(); !reflect.DeepEqual(got, []string{"first", "second"}) {
		t.Fatalf("active leases = %#v", got)
	}
	pool.Disconnect("redis.internal:6379", "first")
	if got := pool.ActiveLeases(); !reflect.DeepEqual(got, []string{"second"}) {
		t.Fatalf("active leases after disconnect = %#v", got)
	}
}

type fakePooledConnection struct {
	closed atomic.Int32
}

func (*fakePooledConnection) Run(pluginapi.Request) (pluginapi.Result, error) {
	return pluginapi.Result{}, nil
}

func (connection *fakePooledConnection) Close() error {
	connection.closed.Add(1)
	return nil
}

func TestConnectionPoolReusesHostPortAcrossLeases(t *testing.T) {
	pool := newConnectionPool(10 * time.Minute)
	connection := &fakePooledConnection{}
	var creates atomic.Int32
	factory := func() (pluginapi.Connection, error) {
		creates.Add(1)
		return connection, nil
	}

	if err := pool.Connect("redis.internal:6379", "redis", "user-a", factory); err != nil {
		t.Fatal(err)
	}
	if err := pool.Connect("redis.internal:6379", "redis", "user-b", factory); err != nil {
		t.Fatal(err)
	}
	acquired, release, err := pool.Acquire("redis.internal:6379", "redis", "user-b", factory)
	if err != nil {
		t.Fatal(err)
	}
	if acquired != connection || creates.Load() != 1 {
		t.Fatalf("connection=%p creates=%d", acquired, creates.Load())
	}
	release()

	pool.Disconnect("redis.internal:6379", "user-a")
	if connection.closed.Load() != 0 {
		t.Fatal("shared connection closed while another lease exists")
	}
	pool.Disconnect("redis.internal:6379", "user-b")
	if connection.closed.Load() != 1 {
		t.Fatalf("close count = %d", connection.closed.Load())
	}
}

func TestConnectionPoolAcquireExistingRequiresConnectedLease(t *testing.T) {
	pool := newConnectionPool(time.Minute)
	if _, _, err := pool.AcquireExisting("redis.internal:6379", "redis", "preset:redis-ci"); !errors.Is(err, errConnectionNotConnected) {
		t.Fatalf("missing connection error = %v", err)
	}

	connection := &fakePooledConnection{}
	if err := pool.Connect("redis.internal:6379", "redis", "preset:redis-ci", func() (pluginapi.Connection, error) { return connection, nil }); err != nil {
		t.Fatal(err)
	}
	acquired, release, err := pool.AcquireExisting("redis.internal:6379", "redis", "preset:redis-ci")
	if err != nil || acquired != connection {
		t.Fatalf("acquired=%p error=%v", acquired, err)
	}
	release()
	pool.Close()
}

func TestConnectionPoolRejectsServiceMismatch(t *testing.T) {
	pool := newConnectionPool(time.Minute)
	connection := &fakePooledConnection{}
	factory := func() (pluginapi.Connection, error) { return connection, nil }
	if err := pool.Connect("database.internal:3000", "aerospike", "first", factory); err != nil {
		t.Fatal(err)
	}
	err := pool.Connect("database.internal:3000", "redis", "second", factory)
	if err == nil || !errors.Is(err, errConnectionServiceMismatch) {
		t.Fatalf("error = %v", err)
	}
	pool.Close()
}

func TestConnectionPoolCreatesOnceForConcurrentConnects(t *testing.T) {
	pool := newConnectionPool(time.Minute)
	connection := &fakePooledConnection{}
	var creates atomic.Int32
	start := make(chan struct{})
	factory := func() (pluginapi.Connection, error) {
		creates.Add(1)
		<-start
		return connection, nil
	}

	var wait sync.WaitGroup
	errorsSeen := make(chan error, 2)
	for _, lease := range []string{"one", "two"} {
		wait.Add(1)
		go func(lease string) {
			defer wait.Done()
			errorsSeen <- pool.Connect("redis.internal:6379", "redis", lease, factory)
		}(lease)
	}
	for creates.Load() == 0 {
		time.Sleep(time.Millisecond)
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatal(err)
		}
	}
	if creates.Load() != 1 {
		t.Fatalf("created %d connections", creates.Load())
	}
	pool.Close()
}

func TestConnectionPoolDefersCloseUntilActiveRequestFinishes(t *testing.T) {
	pool := newConnectionPool(time.Minute)
	connection := &fakePooledConnection{}
	factory := func() (pluginapi.Connection, error) { return connection, nil }
	if err := pool.Connect("redis.internal:6379", "redis", "lease", factory); err != nil {
		t.Fatal(err)
	}
	_, release, err := pool.Acquire("redis.internal:6379", "redis", "lease", factory)
	if err != nil {
		t.Fatal(err)
	}
	pool.Disconnect("redis.internal:6379", "lease")
	if connection.closed.Load() != 0 {
		t.Fatal("active connection closed before release")
	}
	release()
	if connection.closed.Load() != 1 {
		t.Fatalf("close count = %d", connection.closed.Load())
	}
}

func TestConnectionPoolEvictsIdleAndClosesOnShutdown(t *testing.T) {
	now := time.Unix(100, 0)
	pool := newConnectionPool(time.Minute)
	pool.now = func() time.Time { return now }
	first := &fakePooledConnection{}
	second := &fakePooledConnection{}
	if err := pool.Connect("one:6379", "redis", "one", func() (pluginapi.Connection, error) { return first, nil }); err != nil {
		t.Fatal(err)
	}
	if err := pool.Connect("two:3000", "aerospike", "two", func() (pluginapi.Connection, error) { return second, nil }); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	pool.EvictIdle()
	if first.closed.Load() != 1 || second.closed.Load() != 1 {
		t.Fatalf("idle closes = %d, %d", first.closed.Load(), second.closed.Load())
	}

	third := &fakePooledConnection{}
	if err := pool.Connect("three:6379", "redis", "three", func() (pluginapi.Connection, error) { return third, nil }); err != nil {
		t.Fatal(err)
	}
	pool.Close()
	if third.closed.Load() != 1 {
		t.Fatalf("shutdown close count = %d", third.closed.Load())
	}
}

func TestConnectionPoolAutomaticallyClosesIdleConnection(t *testing.T) {
	pool := newConnectionPool(20 * time.Millisecond)
	defer pool.Close()
	connection := &fakePooledConnection{}
	if err := pool.Connect("redis.internal:6379", "redis", "lease", func() (pluginapi.Connection, error) {
		return connection, nil
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for connection.closed.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if connection.closed.Load() != 1 {
		t.Fatalf("idle connection close count = %d", connection.closed.Load())
	}
}

func TestConnectionPoolDoesNotCloseActiveConnection(t *testing.T) {
	pool := newConnectionPool(20 * time.Millisecond)
	defer pool.Close()
	connection := &fakePooledConnection{}
	factory := func() (pluginapi.Connection, error) { return connection, nil }
	if err := pool.Connect("redis.internal:6379", "redis", "lease", factory); err != nil {
		t.Fatal(err)
	}
	_, release, err := pool.Acquire("redis.internal:6379", "redis", "lease", factory)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(50 * time.Millisecond)
	if connection.closed.Load() != 0 {
		t.Fatal("active connection was closed")
	}
	release()
}
