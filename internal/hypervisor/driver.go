package hypervisor

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

var (
	ErrNotConfigured = errors.New("hypervisor driver is not configured")
	ErrInvalidSpec   = errors.New("invalid virtual machine specification")
	ErrNotSupported  = errors.New("operation not supported by hypervisor")
)

type VMState string

const (
	StateUnknown VMState = "unknown"
	StateStopped VMState = "stopped"
	StateRunning VMState = "running"
)

type Spec struct {
	ID        string
	Name      string
	Resources virtual.Resources
}

type Status struct {
	ID    string  `json:"id"`
	State VMState `json:"state"`
}

type Capabilities struct {
	Create       bool `json:"create"`
	Start        bool `json:"start"`
	Stop         bool `json:"stop"`
	Restart      bool `json:"restart"`
	Delete       bool `json:"delete"`
	Status       bool `json:"status"`
	Snapshots    bool `json:"snapshots"`
	Console      bool `json:"console"`
	PublicIP     bool `json:"public_ip"`
}

type Driver interface {
	Name() string
	Capabilities() Capabilities
	Create(context.Context, Spec) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Restart(context.Context, string) error
	Delete(context.Context, string) error
	Status(context.Context, string) (Status, error)
}

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

func (UnavailableDriver) Status(context.Context, string) (Status, error) {
	return Status{}, ErrNotConfigured
}

func ValidateSpec(spec Spec) error {
	if strings.TrimSpace(spec.ID) == "" ||
		strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("%w: ID and name are required", ErrInvalidSpec)
	}

	if spec.Resources.VCPU <= 0 ||
		spec.Resources.MemoryMB <= 0 ||
		spec.Resources.StorageGB <= 0 {
		return fmt.Errorf("%w: CPU, memory and storage must be positive", ErrInvalidSpec)
	}

	return nil
}

func ValidateID(id string) error {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(id) != id {
		return fmt.Errorf("%w: invalid instance ID", ErrInvalidSpec)
	}
	return nil
}
