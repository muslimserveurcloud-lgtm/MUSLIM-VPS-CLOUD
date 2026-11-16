package hypervisor

import (
	"context"
	"errors"
	"testing"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

func TestValidateSpec(t *testing.T) {
	valid := Spec{
		ID:   "vps-test-01",
		Name: "test-vps",
		Resources: virtual.Resources{
			VCPU: 2, MemoryMB: 2048, StorageGB: 20,
		},
	}

	if err := ValidateSpec(valid); err != nil {
		t.Fatalf("valid specification rejected: %v", err)
	}

	invalid := valid
	invalid.ID = ""
	if err := ValidateSpec(invalid); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("expected ErrInvalidSpec, got %v", err)
	}

	invalid = valid
	invalid.Resources.VCPU = 0
	if err := ValidateSpec(invalid); !errors.Is(err, ErrInvalidSpec) {
		t.Fatalf("expected ErrInvalidSpec for CPU, got %v", err)
	}
}

func TestValidateID(t *testing.T) {
	for _, id := range []string{"", " vps-01", "vps-01 "} {
		if err := ValidateID(id); !errors.Is(err, ErrInvalidSpec) {
			t.Errorf("expected invalid ID for %q, got %v", id, err)
		}
	}

	if err := ValidateID("vps-01"); err != nil {
		t.Fatalf("valid ID rejected: %v", err)
	}
}

func TestUnavailableDriver(t *testing.T) {
	driver := UnavailableDriver{}
	ctx := context.Background()

	if driver.Name() != "unavailable" {
		t.Fatalf("unexpected driver name: %s", driver.Name())
	}

	if err := driver.Create(ctx, Spec{}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}

	if err := driver.Start(ctx, "vps-01"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}

	if _, err := driver.Status(ctx, "vps-01"); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("expected ErrNotConfigured, got %v", err)
	}
}
