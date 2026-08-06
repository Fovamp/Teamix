package modelrouter

import (
	"testing"
	"time"

	"reasonix/internal/provider"
)

func TestSelectProviderRoundRobin(t *testing.T) {
	p := &Pool{
		Name:      "pool",
		Providers: []provider.Provider{&fakeProvider{name: "a", text: "a"}, &fakeProvider{name: "b", text: "b"}},
	}
	p.ensure()
	names := map[string]int{}
	for i := 0; i < 6; i++ {
		_, prov := p.SelectProvider()
		names[prov.Name()]++
	}
	if names["a"] != 3 || names["b"] != 3 {
		t.Errorf("round-robin = %v, want a:3 b:3", names)
	}
}

func TestCircuitBreakerSkipsUnhealthy(t *testing.T) {
	p := &Pool{
		Name:      "pool",
		Providers: []provider.Provider{&fakeProvider{name: "a", text: "a"}, &fakeProvider{name: "b", text: "b"}},
	}
	p.ensure()
	p.MarkFailed(0) // a 熔断
	seen := map[string]bool{}
	for i := 0; i < 4; i++ {
		_, prov := p.SelectProvider()
		seen[prov.Name()] = true
	}
	if seen["a"] {
		t.Error("circuit-broken candidate should be skipped")
	}
	if !seen["b"] {
		t.Error("healthy candidate should serve")
	}
}

func TestCircuitBreakerRecoversAfterCooldown(t *testing.T) {
	p := &Pool{
		Name:      "pool",
		Providers: []provider.Provider{&fakeProvider{name: "a", text: "a"}},
	}
	p.ensure()
	p.MarkFailed(0)
	// 直接把熔断截止时间改成过去（模拟冷却结束）
	p.health[0].until.Store(time.Now().Add(-time.Second).UnixNano())
	_, prov := p.SelectProvider()
	if prov.Name() != "a" {
		t.Errorf("after cooldown should recover, got %s", prov.Name())
	}
}

func TestMarkHealthyRecoversImmediately(t *testing.T) {
	p := &Pool{
		Name:      "pool",
		Providers: []provider.Provider{&fakeProvider{name: "a", text: "a"}},
	}
	p.ensure()
	p.MarkFailed(0)
	p.MarkHealthy(0)
	h := p.Health()
	if !h["a"] {
		t.Error("MarkHealthy should recover immediately")
	}
}
