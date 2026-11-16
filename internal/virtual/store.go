package virtual

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var (
	ErrInvalidName           = errors.New("name is required")
	ErrInvalidResources      = errors.New("resources must be positive")
	ErrCapacityNotConfigured = errors.New("resource capacity is not configured")
	ErrInsufficientResources = errors.New("insufficient resources")
	ErrNotFound              = errors.New("allocation not found")
)

type Resources struct {
	VCPU      int `json:"vcpu"`
	MemoryMB  int `json:"memory_mb"`
	StorageGB int `json:"storage_gb"`
}

type Allocation struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Resources Resources `json:"resources"`
}

type diskState struct {
	Allocations []Allocation `json:"allocations"`
}

type Store struct {
	mu       sync.Mutex
	path     string
	capacity Resources
	items    map[string]Allocation
}

func NewStore(path string, capacity Resources) (*Store, error) {
	if path == "" {
		return nil, errors.New("allocation state path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	s := &Store{path: path, capacity: capacity, items: map[string]Allocation{}}
	data, err := os.ReadFile(path)
	if err == nil {
		var state diskState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("decode allocations: %w", err)
		}
		for _, a := range state.Allocations {
			if a.ID == "" {
				return nil, errors.New("stored allocation has empty ID")
			}
			if _, exists := s.items[a.ID]; exists {
				return nil, errors.New("duplicate allocation ID")
			}
			s.items[a.ID] = a
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Create(name string, r Resources) (Allocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "" {
		return Allocation{}, ErrInvalidName
	}
	if r.VCPU <= 0 || r.MemoryMB <= 0 || r.StorageGB <= 0 {
		return Allocation{}, ErrInvalidResources
	}
	if s.capacity.VCPU <= 0 || s.capacity.MemoryMB <= 0 || s.capacity.StorageGB <= 0 {
		return Allocation{}, ErrCapacityNotConfigured
	}
	used := s.usedLocked()
	if used.VCPU+r.VCPU > s.capacity.VCPU || used.MemoryMB+r.MemoryMB > s.capacity.MemoryMB || used.StorageGB+r.StorageGB > s.capacity.StorageGB {
		return Allocation{}, ErrInsufficientResources
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Allocation{}, err
	}
	id := fmt.Sprintf("%x", idBytes)
	a := Allocation{ID: id, Name: name, Resources: r}
	s.items[id] = a
	if err := s.saveLocked(); err != nil {
		delete(s.items, id)
		return Allocation{}, err
	}
	return a, nil
}

func (s *Store) Get(id string) (Allocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.items[id]
	if !ok {
		return Allocation{}, ErrNotFound
	}
	return a, nil
}
func (s *Store) List() []Allocation {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Allocation, 0, len(s.items))
	for _, a := range s.items {
		out = append(out, a)
	}
	return out
}
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.items[id]
	if !ok {
		return ErrNotFound
	}
	delete(s.items, id)
	if err := s.saveLocked(); err != nil {
		s.items[id] = a
		return err
	}
	return nil
}
func (s *Store) Capacity() Resources { return s.capacity }
func (s *Store) Used() Resources     { s.mu.Lock(); defer s.mu.Unlock(); return s.usedLocked() }
func (s *Store) usedLocked() Resources {
	var u Resources
	for _, a := range s.items {
		u.VCPU += a.Resources.VCPU
		u.MemoryMB += a.Resources.MemoryMB
		u.StorageGB += a.Resources.StorageGB
	}
	return u
}
func (s *Store) saveLocked() error {
	state := diskState{Allocations: make([]Allocation, 0, len(s.items))}
	for _, a := range s.items {
		state.Allocations = append(state.Allocations, a)
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".alloc-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}
