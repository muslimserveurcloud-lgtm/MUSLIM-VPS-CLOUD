package virtual

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidName          = errors.New("invalid instance name")
	ErrInvalidResources     = errors.New("invalid resource request")
	ErrCapacityNotConfigured = errors.New("resource capacity not configured")
	ErrInsufficientResources = errors.New("insufficient resources")
	ErrNotFound             = errors.New("allocation not found")
)

type Resources struct {
	VCPU     int `json:"vcpu"`
	MemoryMB int `json:"memory_mb"`
	StorageGB int `json:"storage_gb"`
}

type Allocation struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Resources Resources `json:"resources"`
	CreatedAt time.Time `json:"created_at"`
}

type diskState struct {
	Allocations []Allocation `json:"allocations"`
}

type Store struct {
	mu          sync.Mutex
	path        string
	capacity    Resources
	allocations map[string]Allocation
}

func NewStore(path string, capacity Resources) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("storage path is required")
	}
	if capacity.VCPU < 0 || capacity.MemoryMB < 0 || capacity.StorageGB < 0 {
		return nil, errors.New("capacity cannot be negative")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create storage directory: %w", err)
	}

	s := &Store{
		path:        path,
		capacity:    capacity,
		allocations: make(map[string]Allocation),
	}

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read allocations: %w", err)
	}

	if err == nil {
		var state diskState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("decode allocations: %w", err)
		}
		for _, allocation := range state.Allocations {
			if allocation.ID == "" {
				return nil, errors.New("stored allocation has no ID")
			}
			s.allocations[allocation.ID] = allocation
		}
	} else if err := s.saveLocked(); err != nil {
		return nil, err
	}

	return s, nil
}

func (s *Store) Create(name string, requested Resources) (Allocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return Allocation{}, ErrInvalidName
	}
	if requested.VCPU <= 0 || requested.MemoryMB <= 0 || requested.StorageGB <= 0 {
		return Allocation{}, ErrInvalidResources
	}
	if s.capacity.VCPU <= 0 || s.capacity.MemoryMB <= 0 || s.capacity.StorageGB <= 0 {
		return Allocation{}, ErrCapacityNotConfigured
	}

	used := s.usedLocked()
	if requested.VCPU > s.capacity.VCPU-used.VCPU ||
		requested.MemoryMB > s.capacity.MemoryMB-used.MemoryMB ||
		requested.StorageGB > s.capacity.StorageGB-used.StorageGB {
		return Allocation{}, ErrInsufficientResources
	}

	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return Allocation{}, fmt.Errorf("generate allocation ID: %w", err)
	}

	allocation := Allocation{
		ID:        hex.EncodeToString(idBytes[:]),
		Name:      name,
		Resources: requested,
		CreatedAt: time.Now().UTC(),
	}

	s.allocations[allocation.ID] = allocation
	if err := s.saveLocked(); err != nil {
		delete(s.allocations, allocation.ID)
		return Allocation{}, err
	}

	return allocation, nil
}

func (s *Store) Get(id string) (Allocation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	allocation, ok := s.allocations[id]
	if !ok {
		return Allocation{}, ErrNotFound
	}
	return allocation, nil
}

func (s *Store) List() []Allocation {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]Allocation, 0, len(s.allocations))
	for _, allocation := range s.allocations {
		result = append(result, allocation)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	allocation, ok := s.allocations[id]
	if !ok {
		return ErrNotFound
	}

	delete(s.allocations, id)
	if err := s.saveLocked(); err != nil {
		s.allocations[id] = allocation
		return err
	}
	return nil
}

func (s *Store) Capacity() Resources {
	return s.capacity
}

func (s *Store) Used() Resources {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.usedLocked()
}

func (s *Store) usedLocked() Resources {
	var used Resources
	for _, allocation := range s.allocations {
		used.VCPU += allocation.Resources.VCPU
		used.MemoryMB += allocation.Resources.MemoryMB
		used.StorageGB += allocation.Resources.StorageGB
	}
	return used
}

func (s *Store) saveLocked() error {
	state := diskState{
		Allocations: make([]Allocation, 0, len(s.allocations)),
	}
	for _, allocation := range s.allocations {
		state.Allocations = append(state.Allocations, allocation)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode allocations: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".allocations-*")
	if err != nil {
		return fmt.Errorf("create temporary storage: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write allocations: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync allocations: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close allocations: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("save allocations: %w", err)
	}
	return nil
}
