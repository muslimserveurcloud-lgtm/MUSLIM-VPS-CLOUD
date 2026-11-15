package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

var (
	ErrNotFound        = errors.New("instance not found")
	ErrInvalidState    = errors.New("invalid instance state transition")
	ErrInvalidName     = errors.New("instance name is required")
	ErrStoragePath     = errors.New("storage path is required")
)

const (
	StateStopped = "stopped"
	StateRunning = "running"
)

type Instance struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Resources    virtual.Resources `json:"resources"`
	State        string            `json:"state"`
	RestartCount int               `json:"restart_count"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

type diskState struct {
	Instances []Instance `json:"instances"`
}

type Manager struct {
	mu        sync.Mutex
	path      string
	allocator *virtual.Store
	instances map[string]Instance
}

func NewManager(path string, allocator *virtual.Store) (*Manager, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrStoragePath
	}
	if allocator == nil {
		return nil, errors.New("virtual resource allocator is required")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, fmt.Errorf("create engine storage directory: %w", err)
	}

	m := &Manager{
		path:      path,
		allocator: allocator,
		instances: make(map[string]Instance),
	}

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read instances: %w", err)
	}

	if err == nil {
		var state diskState
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, fmt.Errorf("decode instances: %w", err)
		}

		for _, instance := range state.Instances {
			if instance.ID == "" {
				return nil, errors.New("stored instance has no ID")
			}
			if instance.State != StateRunning && instance.State != StateStopped {
				return nil, errors.New("stored instance has invalid state")
			}
			if _, exists := m.instances[instance.ID]; exists {
				return nil, errors.New("duplicate stored instance ID")
			}
			m.instances[instance.ID] = instance
		}
	} else if err := m.saveLocked(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Manager) Create(name string, resources virtual.Resources) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	name = strings.TrimSpace(name)
	if name == "" {
		return Instance{}, ErrInvalidName
	}

	allocation, err := m.allocator.Create(name, resources)
	if err != nil {
		return Instance{}, err
	}

	now := time.Now().UTC()
	instance := Instance{
		ID:        allocation.ID,
		Name:      allocation.Name,
		Resources: allocation.Resources,
		State:     StateStopped,
		CreatedAt: now,
		UpdatedAt: now,
	}

	m.instances[instance.ID] = instance

	if err := m.saveLocked(); err != nil {
		delete(m.instances, instance.ID)
		if rollbackErr := m.allocator.Delete(instance.ID); rollbackErr != nil {
			return Instance{}, fmt.Errorf(
				"save instance: %w; allocation rollback failed: %v",
				err, rollbackErr,
			)
		}
		return Instance{}, err
	}

	return instance, nil
}

func (m *Manager) Get(id string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, ok := m.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	return instance, nil
}

func (m *Manager) List() []Instance {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]Instance, 0, len(m.instances))
	for _, instance := range m.instances {
		result = append(result, instance)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result
}

func (m *Manager) Start(id string) (Instance, error) {
	return m.transition(id, StateStopped, StateRunning)
}

func (m *Manager) Stop(id string) (Instance, error) {
	return m.transition(id, StateRunning, StateStopped)
}

func (m *Manager) Restart(id string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, ok := m.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	if instance.State != StateRunning {
		return Instance{}, ErrInvalidState
	}

	previous := instance
	instance.RestartCount++
	instance.UpdatedAt = time.Now().UTC()
	m.instances[id] = instance

	if err := m.saveLocked(); err != nil {
		m.instances[id] = previous
		return Instance{}, err
	}

	return instance, nil
}

func (m *Manager) transition(id, expected, next string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, ok := m.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	if instance.State != expected {
		return Instance{}, ErrInvalidState
	}

	previous := instance
	instance.State = next
	instance.UpdatedAt = time.Now().UTC()
	m.instances[id] = instance

	if err := m.saveLocked(); err != nil {
		m.instances[id] = previous
		return Instance{}, err
	}

	return instance, nil
}

func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, ok := m.instances[id]
	if !ok {
		return ErrNotFound
	}
	if instance.State != StateStopped {
		return ErrInvalidState
	}

	delete(m.instances, id)

	if err := m.saveLocked(); err != nil {
		m.instances[id] = instance
		return err
	}

	if err := m.allocator.Delete(id); err != nil {
		m.instances[id] = instance
		if rollbackErr := m.saveLocked(); rollbackErr != nil {
			return fmt.Errorf("delete allocation: %w; restore instance: %v", err, rollbackErr)
		}
		return err
	}

	return nil
}

func (m *Manager) saveLocked() error {
	state := diskState{
		Instances: make([]Instance, 0, len(m.instances)),
	}

	for _, instance := range m.instances {
		state.Instances = append(state.Instances, instance)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode instances: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(m.path), ".instances-*")
	if err != nil {
		return fmt.Errorf("create temporary instance storage: %w", err)
	}

	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write instances: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync instances: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close instances: %w", err)
	}
	if err := os.Rename(tmpName, m.path); err != nil {
		return fmt.Errorf("save instances: %w", err)
	}

	return nil
}
