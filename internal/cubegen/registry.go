// Package cubegen - Registry
package cubegen

import (
	"sync"

	"github.com/tinkler/cube-agent-server/internal/cubegenapi"
)

type Registry struct {
	mu      sync.RWMutex
	sources map[string]cubegenapi.SQLSource
}

func NewRegistry() *Registry {
	return &Registry{sources: map[string]cubegenapi.SQLSource{}}
}

func (r *Registry) Register(cubeName string, src cubegenapi.SQLSource) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sources[cubeName] = src
}

func (r *Registry) Lookup(cubeName string) cubegenapi.SQLSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sources[cubeName]
}

func (r *Registry) Unregister(cubeName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sources, cubeName)
}

func (r *Registry) ListAll() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.sources))
	for k := range r.sources {
		out = append(out, k)
	}
	return out
}