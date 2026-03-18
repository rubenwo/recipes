package llm

import (
	"strings"
	"sync"
	"time"
)

type ProviderConfig struct {
	Host    string
	Model   string
	Timeout time.Duration
}

type ClientPool struct {
	mu      sync.Mutex
	clients []*Client
	next    int
}

func NewClientPool(providers []ProviderConfig) *ClientPool {
	p := &ClientPool{}
	p.buildClients(providers)
	return p
}

func (p *ClientPool) buildClients(providers []ProviderConfig) {
	clients := make([]*Client, 0, len(providers))
	for _, prov := range providers {
		clients = append(clients, NewClient(prov.Host, prov.Model, prov.Timeout))
	}
	p.clients = clients
	p.next = 0
}

func (p *ClientPool) Reload(providers []ProviderConfig) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buildClients(providers)
}

func (p *ClientPool) Acquire() *Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.clients) == 0 {
		return nil
	}
	c := p.clients[p.next%len(p.clients)]
	p.next++
	return c
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
