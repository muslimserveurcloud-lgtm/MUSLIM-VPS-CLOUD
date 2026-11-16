package hypervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type KVMConfig struct {
	URI             string
	StorageDir      string
	BaseImage       string
	BaseImageFormat string
	Network         string
	VirshPath       string
	QemuImgPath     string
}
type KVMDriver struct {
	cfg    KVMConfig
	runner CommandRunner
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

func NewKVMDriver(cfg KVMConfig, runner CommandRunner) (*KVMDriver, error) {
	if strings.TrimSpace(cfg.StorageDir) == "" || strings.TrimSpace(cfg.BaseImage) == "" {
		return nil, fmt.Errorf("KVM storage directory and base image are required")
	}
	if cfg.URI == "" {
		cfg.URI = "qemu:///system"
	}
	if cfg.Network == "" {
		cfg.Network = "default"
	}
	if cfg.VirshPath == "" {
		cfg.VirshPath = "virsh"
	}
	if cfg.QemuImgPath == "" {
		cfg.QemuImgPath = "qemu-img"
	}
	if cfg.BaseImageFormat == "" {
		cfg.BaseImageFormat = "qcow2"
	}
	if cfg.BaseImageFormat != "qcow2" && cfg.BaseImageFormat != "raw" {
		return nil, fmt.Errorf("base image format must be qcow2 or raw")
	}
	var err error
	cfg.StorageDir, err = filepath.Abs(cfg.StorageDir)
	if err != nil {
		return nil, err
	}
	cfg.BaseImage, err = filepath.Abs(cfg.BaseImage)
	if err != nil {
		return nil, err
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	return &KVMDriver{cfg: cfg, runner: runner}, nil
}
func (d *KVMDriver) Name() string { return "kvm" }
func (d *KVMDriver) Capabilities() Capabilities {
	return Capabilities{Create: true, Start: true, Stop: true, Restart: true, Delete: true, Status: true}
}
func (d *KVMDriver) run(ctx context.Context, bin string, args ...string) error {
	out, err := d.runner.Run(ctx, bin, args...)
	if err != nil {
		return fmt.Errorf("%s %v: %w: %s", bin, args, err, strings.TrimSpace(string(out)))
	}
	return nil
}
func (d *KVMDriver) Create(ctx context.Context, s Spec) error {
	if err := ValidateSpec(s); err != nil {
		return err
	}
	if !safeID.MatchString(s.ID) {
		return ErrInvalidSpec
	}
	if _, err := os.Stat(d.cfg.BaseImage); err != nil {
		return fmt.Errorf("base image unavailable: %w", err)
	}
	if err := os.MkdirAll(d.cfg.StorageDir, 0750); err != nil {
		return err
	}
	disk := filepath.Join(d.cfg.StorageDir, s.ID+".qcow2")
	if _, err := os.Stat(disk); err == nil {
		return fmt.Errorf("disk already exists: %s", disk)
	}
	if err := d.run(ctx, d.cfg.QemuImgPath, "create", "-f", "qcow2", "-F", d.cfg.BaseImageFormat, "-b", d.cfg.BaseImage, "-o", fmt.Sprintf("size=%dG", s.Resources.StorageGB), disk); err != nil {
		return err
	}
	xml := fmt.Sprintf(`<domain type='kvm'><name>%s</name><memory unit='MiB'>%d</memory><vcpu>%d</vcpu><os><type arch='x86_64' machine='q35'>hvm</type><boot dev='hd'/></os><features><acpi/><apic/></features><devices><disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='%s'/><target dev='vda' bus='virtio'/></disk><interface type='network'><source network='%s'/><model type='virtio'/></interface><console type='pty'><target type='serial' port='0'/></console></devices></domain>`, s.ID, s.Resources.MemoryMB, s.Resources.VCPU, disk, d.cfg.Network)
	xmlPath := filepath.Join(d.cfg.StorageDir, s.ID+".xml")
	if err := os.WriteFile(xmlPath, []byte(xml), 0600); err != nil {
		_ = os.Remove(disk)
		return err
	}
	err := d.run(ctx, d.cfg.VirshPath, "--connect", d.cfg.URI, "define", xmlPath)
	_ = os.Remove(xmlPath)
	if err != nil {
		_ = os.Remove(disk)
		return err
	}
	return nil
}
func (d *KVMDriver) Start(ctx context.Context, id string) error {
	if !safeID.MatchString(id) {
		return ErrInvalidSpec
	}
	return d.run(ctx, d.cfg.VirshPath, "--connect", d.cfg.URI, "start", id)
}
func (d *KVMDriver) Stop(ctx context.Context, id string) error {
	if !safeID.MatchString(id) {
		return ErrInvalidSpec
	}
	return d.run(ctx, d.cfg.VirshPath, "--connect", d.cfg.URI, "shutdown", id)
}
func (d *KVMDriver) Restart(ctx context.Context, id string) error {
	if !safeID.MatchString(id) {
		return ErrInvalidSpec
	}
	return d.run(ctx, d.cfg.VirshPath, "--connect", d.cfg.URI, "reboot", id)
}
func (d *KVMDriver) Status(ctx context.Context, id string) (State, error) {
	if !safeID.MatchString(id) {
		return StateUnknown, ErrInvalidSpec
	}
	out, err := d.runner.Run(ctx, d.cfg.VirshPath, "--connect", d.cfg.URI, "domstate", id)
	if err != nil {
		return StateUnknown, fmt.Errorf("virsh domstate: %w: %s", err, strings.TrimSpace(string(out)))
	}
	v := strings.ToLower(strings.TrimSpace(string(out)))
	if strings.Contains(v, "running") || v == "idle" {
		return StateRunning, nil
	}
	if strings.Contains(v, "shut off") || v == "shutoff" || v == "crashed" {
		return StateStopped, nil
	}
	return StateUnknown, nil
}
func (d *KVMDriver) Delete(ctx context.Context, id string) error {
	state, err := d.Status(ctx, id)
	if err != nil {
		return err
	}
	if state == StateRunning {
		if err := d.run(ctx, d.cfg.VirshPath, "--connect", d.cfg.URI, "destroy", id); err != nil {
			return err
		}
	}
	if err := d.run(ctx, d.cfg.VirshPath, "--connect", d.cfg.URI, "undefine", id); err != nil {
		return err
	}
	return os.Remove(filepath.Join(d.cfg.StorageDir, id+".qcow2"))
}
