package upstream

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"ai-api-stronger/internal/planner"
)

type ClientKey struct {
	ChannelName  string
	BaseURL      string
	Format       string
	Timeout      time.Duration
	ProxyType    string
	ProxyAddress string
}

type ManagedClient struct {
	key      ClientKey
	client   *http.Client
	active   atomic.Int64
	draining atomic.Bool
}

type Manager struct {
	mu      sync.Mutex
	clients map[ClientKey]*ManagedClient
}

type Stats struct {
	ClientCount       int   `json:"client_count"`
	ActiveConnections int64 `json:"active_connections"`
	DrainingCount     int   `json:"draining_count"`
}

func NewManager() *Manager { return &Manager{clients: map[ClientKey]*ManagedClient{}} }

func KeyFromPlan(plan *planner.ExecutionPlan) ClientKey {
	clientKey := ClientKey{ChannelName: plan.ChannelName, BaseURL: plan.UpstreamURL, Format: plan.UpstreamFormat, Timeout: plan.Timeout}
	if plan.Proxy != nil {
		clientKey.ProxyType = plan.Proxy.Type
		clientKey.ProxyAddress = plan.Proxy.Address
	}
	return clientKey
}

func (m *Manager) Acquire(key ClientKey) (*ManagedClient, func()) {
	m.mu.Lock()
	managedClient := m.clients[key]
	if managedClient == nil || managedClient.draining.Load() {
		managedClient = &ManagedClient{key: key, client: &http.Client{Timeout: key.Timeout, Transport: NewTransport(key)}}
		m.clients[key] = managedClient
	}
	managedClient.active.Add(1)
	m.mu.Unlock()
	return managedClient, func() { m.release(managedClient) }
}

func (m *Manager) release(managedClient *ManagedClient) {
	if managedClient.active.Add(-1) == 0 && managedClient.draining.Load() {
		managedClient.client.CloseIdleConnections()
	}
}

func (mc *ManagedClient) Client() *http.Client { return mc.client }

func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	var stats Stats
	stats.ClientCount = len(m.clients)
	for _, managedClient := range m.clients {
		stats.ActiveConnections += managedClient.active.Load()
		if managedClient.draining.Load() {
			stats.DrainingCount++
		}
	}
	return stats
}

func (m *Manager) MarkAllDraining() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, managedClient := range m.clients {
		managedClient.draining.Store(true)
		if managedClient.active.Load() == 0 {
			managedClient.client.CloseIdleConnections()
		}
	}
}
