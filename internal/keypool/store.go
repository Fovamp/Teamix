package keypool

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// poolFile is the on-disk representation of a key pool.
type poolFile struct {
	Strategy string `yaml:"strategy"`
	Keys     []Key  `yaml:"keys"`
}

func (p *Pool) Save(root string) error {
	p.mu.RLock()
	entries := make([]Key, len(p.keys))
	for i, k := range p.keys {
		entries[i] = *k
	}
	strat := p.strategy
	p.mu.RUnlock()

	pf := poolFile{
		Strategy: string(strat),
		Keys:     entries,
	}
	data, err := yaml.Marshal(&pf)
	if err != nil {
		return err
	}

	dir := filepath.Join(root, ".teamix", "secrets")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "pool.yaml")
	return os.WriteFile(path, data, 0600)
}

// Load reads the persisted pool from .teamix/secrets/pool.yaml.
// If the file does not exist, the pool remains empty.
func (p *Pool) Load(root string) error {
	path := filepath.Join(root, ".teamix", "secrets", "pool.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var pf poolFile
	if err := yaml.Unmarshal(data, &pf); err != nil {
		return err
	}

	keys := make([]*Key, len(pf.Keys))
	for i := range pf.Keys {
		keys[i] = &pf.Keys[i]
	}

	p.SetKeys(keys)
	p.SetStrategy(Strategy(pf.Strategy))
	return nil
}
