package settings

import (
	"os"
	"sync"

	"github.com/jacoknapp/scriptorum/internal/config"
	"gopkg.in/yaml.v3"
)

type Store struct {
	mu   sync.RWMutex
	path string
	cfg  *config.Config
}

func New(path string, cfg *config.Config) *Store { return &Store{path: path, cfg: cfg} }
func (s *Store) Get() *config.Config             { s.mu.RLock(); defer s.mu.RUnlock(); return s.cfg }

func (s *Store) Update(newCfg *config.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := yaml.Marshal(newCfg)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, b, 0o644); err != nil {
		return err
	}
	s.cfg = newCfg
	return nil
}
