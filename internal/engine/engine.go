package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/hypervisor"
	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

var (
	ErrNotFound     = errors.New("instance not found")
	ErrInvalidState = errors.New("invalid instance state transition")
	ErrInvalidName  = errors.New("instance name is required")
	ErrStoragePath  = errors.New("storage path is required")
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
	driver    hypervisor.Driver
	instances map[string]Instance
}

func NewManager(path string, a *virtual.Store) (*Manager, error) {
	return NewManagerWithDriver(path, a, nil)
}
func NewManagerWithDriver(path string, a *virtual.Store, d hypervisor.Driver) (*Manager, error) {
	if strings.TrimSpace(path) == "" {
		return nil, ErrStoragePath
	}
	if a == nil {
		return nil, errors.New("virtual resource allocator is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return nil, err
	}
	m := &Manager{path: path, allocator: a, driver: d, instances: map[string]Instance{}}
	b, err := os.ReadFile(path)
	if err == nil {
		var state diskState
		if err = json.Unmarshal(b, &state); err != nil {
			return nil, err
		}
		for _, i := range state.Instances {
			if i.ID == "" || (i.State != StateRunning && i.State != StateStopped) {
				return nil, errors.New("invalid stored instance")
			}
			if _, ok := m.instances[i.ID]; ok {
				return nil, errors.New("duplicate instance ID")
			}
			m.instances[i.ID] = i
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err = m.saveLocked(); err != nil {
		return nil, err
	}
	return m, nil
}
func (m *Manager) DriverName() string {
	if m.driver == nil {
		return "simulation"
	}
	return m.driver.Name()
}
func (m *Manager) Create(name string, r virtual.Resources) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return Instance{}, ErrInvalidName
	}
	a, err := m.allocator.Create(name, r)
	if err != nil {
		return Instance{}, err
	}
	i := Instance{ID: a.ID, Name: a.Name, Resources: a.Resources, State: StateStopped, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if m.driver != nil {
		if err = m.driver.Create(context.Background(), hypervisor.Spec{ID: i.ID, Name: i.Name, Resources: i.Resources}); err != nil {
			_ = m.allocator.Delete(i.ID)
			return Instance{}, err
		}
	}
	m.instances[i.ID] = i
	if err = m.saveLocked(); err != nil {
		delete(m.instances, i.ID)
		if m.driver != nil {
			_ = m.driver.Delete(context.Background(), i.ID)
		}
		_ = m.allocator.Delete(i.ID)
		return Instance{}, err
	}
	return i, nil
}
func (m *Manager) Get(id string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	return i, nil
}
func (m *Manager) List() []Instance {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Instance, 0, len(m.instances))
	for _, i := range m.instances {
		out = append(out, i)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.Before(out[b].CreatedAt) })
	return out
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
	i, ok := m.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	if i.State != StateRunning {
		return Instance{}, ErrInvalidState
	}
	if m.driver != nil {
		if err := m.driver.Restart(context.Background(), id); err != nil {
			return Instance{}, err
		}
	}
	old := i
	i.RestartCount++
	i.UpdatedAt = time.Now().UTC()
	m.instances[id] = i
	if err := m.saveLocked(); err != nil {
		m.instances[id] = old
		return Instance{}, err
	}
	return i, nil
}
func (m *Manager) transition(id, from, to string) (Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.instances[id]
	if !ok {
		return Instance{}, ErrNotFound
	}
	if i.State != from {
		return Instance{}, ErrInvalidState
	}
	if m.driver != nil {
		var err error
		if to == StateRunning {
			err = m.driver.Start(context.Background(), id)
		} else {
			err = m.driver.Stop(context.Background(), id)
		}
		if err != nil {
			return Instance{}, err
		}
	}
	old := i
	i.State = to
	i.UpdatedAt = time.Now().UTC()
	m.instances[id] = i
	if err := m.saveLocked(); err != nil {
		m.instances[id] = old
		if m.driver != nil {
			if to == StateRunning {
				_ = m.driver.Stop(context.Background(), id)
			} else {
				_ = m.driver.Start(context.Background(), id)
			}
		}
		return Instance{}, err
	}
	return i, nil
}
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, ok := m.instances[id]
	if !ok {
		return ErrNotFound
	}
	if i.State != StateStopped {
		return ErrInvalidState
	}
	if m.driver != nil {
		if err := m.driver.Delete(context.Background(), id); err != nil {
			return err
		}
	}
	if err := m.allocator.Delete(id); err != nil {
		return err
	}
	delete(m.instances, id)
	if err := m.saveLocked(); err != nil {
		m.instances[id] = i
		return err
	}
	return nil
}
func (m *Manager) saveLocked() error {
	s := diskState{Instances: make([]Instance, 0, len(m.instances))}
	for _, i := range m.instances {
		s.Instances = append(s.Instances, i)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(m.path), ".instances-*")
	if err != nil {
		return err
	}
	n := tmp.Name()
	defer os.Remove(n)
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
	return os.Rename(n, m.path)
}
