package llm

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/rubenwo/mise/internal/database"
)


type ProviderConfig struct {
	Host         string
	Model        string
	ProviderType ProviderType
	Timeout      time.Duration
	ProviderID   int
	Tags         []string
}

type ClientPool struct {
	mu             sync.Mutex
	clients        []*Client
	next           int
	healthCheck    time.Duration
	stopHealthChan chan struct{}
}

func NewClientPool(providers []ProviderConfig) *ClientPool {
	p := &ClientPool{}
	p.buildClients(providers)
	return p
}

func (p *ClientPool) buildClients(providers []ProviderConfig) {
	clients := make([]*Client, 0, len(providers))
	for _, prov := range providers {
		clients = append(clients, NewClient(prov.Host, prov.Model, prov.ProviderType, prov.Timeout, prov.ProviderID, prov.Tags))
	}
	p.clients = clients
	p.next = 0
}

func (p *ClientPool) Reload(providers []ProviderConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buildClients(providers)
}

// FeatureStatus returns the availability of a feature tag:
//   "available"    — at least one healthy provider has this tag
//   "offline"      — a provider has the tag but none are currently healthy
//   "unconfigured" — no provider has this tag at all
func (p *ClientPool) FeatureStatus(tag string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	configured := false
	for _, c := range p.clients {
		if c.hasTag(tag) {
			configured = true
			if c.healthy.Load() {
				return "available"
			}
		}
	}
	if configured {
		return "offline"
	}
	return "unconfigured"
}

// AcquireWithTag returns a healthy client that has the given tag, using
// round-robin across matching clients. Returns nil if no healthy tagged
// client exists — callers must handle nil and return an appropriate error.
// There is no fallback to untagged providers.
func (p *ClientPool) AcquireWithTag(tag string) *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	startIdx := p.next
	for i := 0; i < len(p.clients); i++ {
		idx := (startIdx + i) % len(p.clients)
		c := p.clients[idx]
		if c.healthy.Load() && c.hasTag(tag) {
			p.next = idx + 1
			return c
		}
	}
	return nil
}

func (p *ClientPool) Model() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.clients) == 0 {
		return ""
	}
	if len(p.clients) == 1 {
		return p.clients[0].Model()
	}
	models := make([]string, len(p.clients))
	for i, c := range p.clients {
		models[i] = c.Model()
	}
	return strings.Join(models, ", ")
}

func (p *ClientPool) Clients() []*Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Client, len(p.clients))
	copy(out, p.clients)
	return out
}

func (p *ClientPool) StartHealthChecker(ctx context.Context, interval time.Duration, db *database.Queries) {
	p.healthCheck = interval
	p.stopHealthChan = make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				p.checkHealth(ctx, db)
			case <-p.stopHealthChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (p *ClientPool) StopHealthChecker() {
	if p.stopHealthChan != nil {
		close(p.stopHealthChan)
	}
}

func (p *ClientPool) checkHealth(ctx context.Context, db *database.Queries) {
	p.mu.Lock()
	clients := make([]*Client, len(p.clients))
	copy(clients, p.clients)
	p.mu.Unlock()

	for _, c := range clients {
		healthy := c.IsHealthy(ctx)
		c.lastCheck = time.Now()
		c.healthy.Store(healthy)

		var lastError *string
		if !healthy {
			err := "health check failed"
			lastError = &err
		}

		status := "healthy"
		if !healthy {
			status = "unhealthy"
		}

		_ = db.UpdateProviderHealthStatus(ctx, c.providerID, status, lastError)
	}
}
