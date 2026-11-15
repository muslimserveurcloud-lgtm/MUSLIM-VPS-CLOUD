package virtual

import (
	"errors"
	"path/filepath"
	"testing"
)

func testCapacity() Resources {
	return Resources{VCPU: 8, MemoryMB: 8192, StorageGB: 100}
}

func TestCreateAndPersistAllocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allocations.json")

	store, err := NewStore(path, testCapacity())
	if err != nil {
		t.Fatal(err)
	}

	created, err := store.Create("test-vps", Resources{
		VCPU: 2, MemoryMB: 2048, StorageGB: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("expected generated allocation ID")
	}

	reopened, err := NewStore(path, testCapacity())
	if err != nil {
		t.Fatal(err)
	}

	found, err := reopened.Get(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.Name != "test-vps" {
		t.Fatalf("expected test-vps, got %q", found.Name)
	}
	if got := reopened.Used(); got.VCPU != 2 || got.MemoryMB != 2048 || got.StorageGB != 20 {
		t.Fatalf("unexpected resource usage: %+v", got)
	}
}

func TestRejectInsufficientResources(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "allocations.json"), testCapacity())
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Create("too-large", Resources{
		VCPU: 9, MemoryMB: 1024, StorageGB: 10,
	})
	if !errors.Is(err, ErrInsufficientResources) {
		t.Fatalf("expected ErrInsufficientResources, got %v", err)
	}
}

func TestDeleteReleasesResources(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "allocations.json"), testCapacity())
	if err != nil {
		t.Fatal(err)
	}

	created, err := store.Create("delete-me", Resources{
		VCPU: 2, MemoryMB: 1024, StorageGB: 10,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.Delete(created.ID); err != nil {
		t.Fatal(err)
	}
	if got := store.Used(); got != (Resources{}) {
		t.Fatalf("expected released resources, got %+v", got)
	}
}

func TestRejectInvalidResources(t *testing.T) {
	store, err := NewStore(filepath.Join(t.TempDir(), "allocations.json"), testCapacity())
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Create("invalid", Resources{VCPU: 0, MemoryMB: 512, StorageGB: 5})
	if !errors.Is(err, ErrInvalidResources) {
		t.Fatalf("expected ErrInvalidResources, got %v", err)
	}
}
