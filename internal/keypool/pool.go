package keypool

import (
	"log/slog"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
)

type Strategy string

const (
	StrategyRoundRobin Strategy = "round-robin"
	StrategyRandom     Strategy = "random"
)

type Key struct {
	EnvName  string `yaml:"env_name" json:"envName"`
	Value    string `yaml:"value"    json:"-"`
	Enabled  bool   `yaml:"enabled"  json:"enabled"`
	UseCount int64  `yaml:"-"        json:"useCount"`
}

type KeyInfo struct {
	EnvName  string `json:"envName"`
	Enabled  bool   `json:"enabled"`
	UseCount int64  `json:"useCount"`
	Selected bool   `json:"selected"`
}

type Pool struct {
	mu        sync.RWMutex
	keys      []*Key
	strategy  Strategy
	counter   atomic.Int64
	targetEnv string
}

func NewPool(targetEnv string) *Pool {
	return &Pool{
		keys:      make([]*Key, 0),
		strategy:  StrategyRoundRobin,
		targetEnv: targetEnv,
	}
}

func (p *Pool) SetStrategy(s Strategy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.strategy = s
}

func (p *Pool) Strategy() Strategy {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.strategy
}

func (p *Pool) AddKey(envName, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if envName == "" {
		envName = p.targetEnv + "_" + nextSuffix(p.keys)
	}
	p.keys = append(p.keys, &Key{
		EnvName: envName,
		Value:   value,
		Enabled: true,
	})
	slog.Info("keypool: key added", "env", envName, "pool_size", len(p.keys))
}

func (p *Pool) RemoveKey(envName string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, k := range p.keys {
		if k.EnvName == envName {
			p.keys = append(p.keys[:i], p.keys[i+1:]...)
			return true
		}
	}
	return false
}

func (p *Pool) SetKeyEnabled(envName string, enabled bool) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range p.keys {
		if k.EnvName == envName {
			k.Enabled = enabled
			return true
		}
	}
	return false
}

func (p *Pool) SetKeyValue(envName, value string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, k := range p.keys {
		if k.EnvName == envName {
			k.Value = value
			return true
		}
	}
	return false
}

func (p *Pool) Count() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	n := 0
	for _, k := range p.keys {
		if k.Enabled {
			n++
		}
	}
	return n
}

func (p *Pool) Acquire() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	enabled := make([]*Key, 0, len(p.keys))
	for _, k := range p.keys {
		if k.Enabled {
			enabled = append(enabled, k)
		}
	}
	if len(enabled) == 0 {
		slog.Warn("keypool: no enabled keys available")
		os.Unsetenv(p.targetEnv)
		return ""
	}

	var chosen *Key
	switch p.strategy {
	case StrategyRandom:
		chosen = enabled[rand.Intn(len(enabled))]
	default:
		idx := int(p.counter.Add(1) % int64(len(enabled)))
		chosen = enabled[idx]
	}

	atomic.AddInt64(&chosen.UseCount, 1)
	os.Setenv(p.targetEnv, chosen.Value)
	slog.Debug("keypool: acquired key", "env", chosen.EnvName, "use_count", chosen.UseCount)
	return chosen.EnvName
}

func (p *Pool) List() []KeyInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()
	active := os.Getenv(p.targetEnv)
	out := make([]KeyInfo, len(p.keys))
	for i, k := range p.keys {
		out[i] = KeyInfo{
			EnvName:  k.EnvName,
			Enabled:  k.Enabled,
			UseCount: atomic.LoadInt64(&k.UseCount),
			Selected: k.Value != "" && k.Value == active,
		}
	}
	return out
}

func (p *Pool) AllKeys() []*Key {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]*Key, len(p.keys))
	copy(out, p.keys)
	return out
}

func (p *Pool) SetKeys(keys []*Key) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keys = keys
}

func (p *Pool) TargetEnv() string { return p.targetEnv }

func nextSuffix(keys []*Key) string {
	seen := make(map[string]bool)
	for _, k := range keys {
		seen[k.EnvName] = true
	}
	for i := 1; ; i++ {
		s := suffixFromInt(i)
		if !seen[s] {
			return s
		}
	}
}

func suffixFromInt(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
