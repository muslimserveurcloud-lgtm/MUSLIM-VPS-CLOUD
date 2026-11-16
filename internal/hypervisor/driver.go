package hypervisor

import (
	"context"
	"errors"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

var (
	ErrNotConfigured = errors.New("hypervisor is not configured")
	ErrInvalidSpec   = errors.New("invalid virtual machine specification")
	ErrNotSupported  = errors.New("operation is not supported by this driver")
)

type Spec struct {
	ID        string
	Name      string
	Resources virtual.Resources
}
type State string

const (
	StateUnknown State = "unknown"
	StateStopped State = "stopped"
	StateRunning State = "running"
)

type Capabilities struct {
	Create    bool `json:"create"`
	Start     bool `json:"start"`
	Stop      bool `json:"stop"`
	Restart   bool `json:"restart"`
	Delete    bool `json:"delete"`
	Status    bool `json:"status"`
	Snapshots bool `json:"snapshots"`
	Console   bool `json:"console"`
	PublicIP  bool `json:"public_ip"`
}
type Driver interface {
	Name() string
	Capabilities() Capabilities
	Create(context.Context, Spec) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Restart(context.Context, string) error
	Delete(context.Context, string) error
	Status(context.Context, string) (State, error)
}

func ValidateSpec(s Spec) error {
	if s.ID == "" || s.Name == "" || s.Resources.VCPU <= 0 || s.Resources.MemoryMB <= 0 || s.Resources.StorageGB <= 0 {
		return ErrInvalidSpec
	}
	return nil
}

// ValidateID checks whether a virtual machine ID is valid.
func ValidateID(id string) error {
    if id == "" {
        return ErrInvalidSpec
    }
    for _, r := range id {
        if !((r >= 'a' && r <= 'z') ||
             (r >= 'A' && r <= 'Z') ||
             (r >= '0' && r <= '9') ||
             r == '-' || r == '_') {
            return ErrInvalidSpec
        }
    }
    return nil
}

// UnavailableDriver represents a host without a configured hypervisor.
type UnavailableDriver struct{}

func (UnavailableDriver) Name() string {
	return "unavailable"
}

func (UnavailableDriver) Capabilities() Capabilities {
	return Capabilities{}
}

func (UnavailableDriver) Create(context.Context, Spec) error {
	return ErrNotConfigured
}

func (UnavailableDriver) Start(context.Context, string) error {
	return ErrNotConfigured
}

func (UnavailableDriver) Stop(context.Context, string) error {
	return ErrNotConfigured
}

func (UnavailableDriver) Restart(context.Context, string) error {
	return ErrNotConfigured
}

func (UnavailableDriver) Delete(context.Context, string) error {
	return ErrNotConfigured
}

func (UnavailableDriver) Status(context.Context, string) (State, error) {
	return StateUnknown, ErrNotConfigured
}
