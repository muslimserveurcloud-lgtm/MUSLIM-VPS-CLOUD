package engine

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()

	dir := t.TempDir()

	allocator, err := virtual.NewStore(
		filepath.Join(dir, "allocations.json"),
		virtual.Resources{
			VCPU: 8, MemoryMB: 8192, StorageGB: 100,
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(filepath.Join(dir, "instances.json"), allocator)
	if err != nil {
		t.Fatal(err)
	}

	return manager
}

func createTestInstance(t *testing.T, manager *Manager) Instance {
	t.Helper()

	instance, err := manager.Create("test-vps", virtual.Resources{
		VCPU: 2, MemoryMB: 2048, StorageGB: 20,
	})
	if err != nil {
		t.Fatal(err)
	}

	return instance
}

func TestInstanceLifecycle(t *testing.T) {
	manager := newTestManager(t)
	instance := createTestInstance(t, manager)

	if instance.State != StateStopped {
		t.Fatalf("expected stopped, got %s", instance.State)
	}

	started, err := manager.Start(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if started.State != StateRunning {
		t.Fatalf("expected running, got %s", started.State)
	}

	restarted, err := manager.Restart(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.RestartCount != 1 {
		t.Fatalf("expected one restart, got %d", restarted.RestartCount)
	}

	stopped, err := manager.Stop(instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != StateStopped {
		t.Fatalf("expected stopped, got %s", stopped.State)
	}

	if err := manager.Delete(instance.ID); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.Get(instance.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRejectInvalidTransitions(t *testing.T) {
	manager := newTestManager(t)
	instance := createTestInstance(t, manager)

	if _, err := manager.Stop(instance.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState when stopping stopped instance, got %v", err)
	}

	if _, err := manager.Restart(instance.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState when restarting stopped instance, got %v", err)
	}

	if err := manager.Delete(instance.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCannotDeleteRunningInstance(t *testing.T) {
	manager := newTestManager(t)
	instance := createTestInstance(t, manager)

	if _, err := manager.Start(instance.ID); err != nil {
		t.Fatal(err)
	}

	if err := manager.Delete(instance.ID); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState, got %v", err)
	}
}

func TestInstancePersistence(t *testing.T) {
	dir := t.TempDir()
	allocationPath := filepath.Join(dir, "allocations.json")
	instancePath := filepath.Join(dir, "instances.json")

	allocator, err := virtual.NewStore(allocationPath, virtual.Resources{
		VCPU: 8, MemoryMB: 8192, StorageGB: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	manager, err := NewManager(instancePath, allocator)
	if err != nil {
		t.Fatal(err)
	}

	created := createTestInstance(t, manager)

	reopenedAllocator, err := virtual.NewStore(allocationPath, virtual.Resources{
		VCPU: 8, MemoryMB: 8192, StorageGB: 100,
	})
	if err != nil {
		t.Fatal(err)
	}

	reopened, err := NewManager(instancePath, reopenedAllocator)
	if err != nil {
		t.Fatal(err)
	}

	found, err := reopened.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Name != created.Name || found.State != StateStopped {
		t.Fatalf("unexpected restored instance: %+v", found)
	}
}

func TestRejectEmptyName(t *testing.T) {
	manager := newTestManager(t)

	_, err := manager.Create(" ", virtual.Resources{
		VCPU: 1, MemoryMB: 512, StorageGB: 5,
	})
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("expected ErrInvalidName, got %v", err)
	}
}
