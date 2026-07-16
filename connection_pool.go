package main

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	pluginapi "pluginvm/plugins"
)

func (pool *connectionPool) ActiveLeases() []string {
	pool.EvictIdle()
	pool.mu.Lock()
	active := make([]string, 0)
	seen := map[string]struct{}{}
	for _, entry := range pool.connections {
		for lease := range entry.leases {
			if _, exists := seen[lease]; exists {
				continue
			}
			seen[lease] = struct{}{}
			active = append(active, lease)
		}
	}
	pool.mu.Unlock()
	sort.Strings(active)
	return active
}

var (
	errConnectionServiceMismatch = errors.New("connection service mismatch")
	errConnectionPoolClosed      = errors.New("connection pool closed")
	errConnectionNotConnected    = errors.New("Connect this preset first")
)

type pooledConnection struct {
	owner         string
	connection    pluginapi.Connection
	leases        map[string]struct{}
	active        int
	lastUsed      time.Time
	closeWhenIdle bool
}

type connectionCreation struct {
	done chan struct{}
	err  error
}

type connectionPool struct {
	mu          sync.Mutex
	connections map[string]*pooledConnection
	creating    map[string]*connectionCreation
	idleTimeout time.Duration
	now         func() time.Time
	stop        chan struct{}
	done        chan struct{}
	stopOnce    sync.Once
	closed      bool
}

func leaseName(lease string) string {
	if lease == "" {
		return "anonymous"
	}
	return lease
}

func newConnectionPool(idleTimeout time.Duration) *connectionPool {
	pool := &connectionPool{
		connections: map[string]*pooledConnection{},
		creating:    map[string]*connectionCreation{},
		idleTimeout: idleTimeout,
		now:         time.Now,
		stop:        make(chan struct{}),
		done:        make(chan struct{}),
	}
	go pool.closeIdleConnections()
	return pool
}

func (pool *connectionPool) closeIdleConnections() {
	interval := pool.idleTimeout / 10
	if interval > time.Second {
		interval = time.Second
	}
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	defer close(pool.done)
	for {
		select {
		case <-ticker.C:
			pool.EvictIdle()
		case <-pool.stop:
			return
		}
	}
}

func (pool *connectionPool) Connect(key, owner, lease string, factory func() (pluginapi.Connection, error)) error {
	pool.EvictIdle()
	_, err := pool.get(key, owner, lease, false, factory)
	return err
}

func (pool *connectionPool) Acquire(key, owner, lease string, factory func() (pluginapi.Connection, error)) (pluginapi.Connection, func(), error) {
	pool.EvictIdle()
	entry, err := pool.get(key, owner, lease, true, factory)
	if err != nil {
		return nil, nil, err
	}
	var once sync.Once
	release := func() {
		once.Do(func() { pool.release(key, entry) })
	}
	return entry.connection, release, nil
}

func (pool *connectionPool) AcquireExisting(key, owner, lease string) (pluginapi.Connection, func(), error) {
	pool.EvictIdle()
	lease = leaseName(lease)
	pool.mu.Lock()
	if pool.closed {
		pool.mu.Unlock()
		return nil, nil, errConnectionPoolClosed
	}
	entry := pool.connections[key]
	if entry == nil {
		pool.mu.Unlock()
		return nil, nil, errConnectionNotConnected
	}
	if entry.owner != owner {
		pool.mu.Unlock()
		return nil, nil, fmt.Errorf("%w: %s is already connected by %s", errConnectionServiceMismatch, key, entry.owner)
	}
	if _, connected := entry.leases[lease]; !connected {
		pool.mu.Unlock()
		return nil, nil, errConnectionNotConnected
	}
	entry.active++
	entry.lastUsed = pool.now()
	pool.mu.Unlock()
	var once sync.Once
	release := func() { once.Do(func() { pool.release(key, entry) }) }
	return entry.connection, release, nil
}

func (pool *connectionPool) get(key, owner, lease string, active bool, factory func() (pluginapi.Connection, error)) (*pooledConnection, error) {
	lease = leaseName(lease)
	for {
		pool.mu.Lock()
		if pool.closed {
			pool.mu.Unlock()
			return nil, errConnectionPoolClosed
		}
		if entry := pool.connections[key]; entry != nil {
			if entry.owner != owner {
				pool.mu.Unlock()
				return nil, fmt.Errorf("%w: %s is already connected by %s", errConnectionServiceMismatch, key, entry.owner)
			}
			entry.leases[lease] = struct{}{}
			entry.lastUsed = pool.now()
			entry.closeWhenIdle = false
			if active {
				entry.active++
			}
			pool.mu.Unlock()
			return entry, nil
		}
		if creation := pool.creating[key]; creation != nil {
			done := creation.done
			pool.mu.Unlock()
			<-done
			if creation.err != nil {
				return nil, creation.err
			}
			continue
		}
		creation := &connectionCreation{done: make(chan struct{})}
		pool.creating[key] = creation
		pool.mu.Unlock()

		connection, err := factory()
		if err == nil && connection == nil {
			err = errors.New("plugin returned a nil connection")
		}

		pool.mu.Lock()
		delete(pool.creating, key)
		if err == nil && pool.closed {
			err = errConnectionPoolClosed
		}
		if err == nil {
			pool.connections[key] = &pooledConnection{
				owner: owner, connection: connection, leases: map[string]struct{}{}, lastUsed: pool.now(),
			}
		}
		creation.err = err
		close(creation.done)
		pool.mu.Unlock()
		if err != nil {
			if connection != nil {
				_ = connection.Close()
			}
			return nil, err
		}
	}
}

func (pool *connectionPool) Disconnect(key, lease string) {
	lease = leaseName(lease)
	var closeConnection pluginapi.Connection
	pool.mu.Lock()
	if entry := pool.connections[key]; entry != nil {
		delete(entry.leases, lease)
		entry.lastUsed = pool.now()
		if len(entry.leases) == 0 {
			if entry.active == 0 {
				delete(pool.connections, key)
				closeConnection = entry.connection
			} else {
				entry.closeWhenIdle = true
			}
		}
	}
	pool.mu.Unlock()
	if closeConnection != nil {
		_ = closeConnection.Close()
	}
}

func (pool *connectionPool) release(key string, expected *pooledConnection) {
	var closeConnection pluginapi.Connection
	pool.mu.Lock()
	if entry := pool.connections[key]; entry == expected {
		if entry.active > 0 {
			entry.active--
		}
		entry.lastUsed = pool.now()
		if entry.active == 0 && entry.closeWhenIdle && len(entry.leases) == 0 {
			delete(pool.connections, key)
			closeConnection = entry.connection
		}
	}
	pool.mu.Unlock()
	if closeConnection != nil {
		_ = closeConnection.Close()
	}
}

func (pool *connectionPool) EvictIdle() {
	now := pool.now()
	var closing []pluginapi.Connection
	pool.mu.Lock()
	for key, entry := range pool.connections {
		if entry.active == 0 && now.Sub(entry.lastUsed) >= pool.idleTimeout {
			delete(pool.connections, key)
			closing = append(closing, entry.connection)
		}
	}
	pool.mu.Unlock()
	for _, connection := range closing {
		_ = connection.Close()
	}
}

func (pool *connectionPool) Close() {
	var closing []pluginapi.Connection
	pool.mu.Lock()
	if !pool.closed {
		pool.closed = true
		for key, entry := range pool.connections {
			delete(pool.connections, key)
			closing = append(closing, entry.connection)
		}
	}
	pool.mu.Unlock()
	for _, connection := range closing {
		_ = connection.Close()
	}
	pool.stopOnce.Do(func() { close(pool.stop) })
	<-pool.done
}
