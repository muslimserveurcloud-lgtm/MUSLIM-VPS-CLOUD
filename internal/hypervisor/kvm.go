package hypervisor

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrKVMUnavailable = errors.New("KVM/libvirt tools are unavailable")
	ErrImageRequired  = errors.New("a Linux base image is required")
)

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type InputRunner interface {
	RunInput(context.Context, string, []byte, ...string) ([]byte, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (ExecRunner) RunInput(ctx context.Context, name string, input []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
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
	config KVMConfig
	runner CommandRunner
}

var safeID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,62}$`)

func NewKVMDriver(config KVMConfig, runner CommandRunner) (*KVMDriver, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	if strings.TrimSpace(config.URI) == "" {
		config.URI = "qemu:///system"
	}
	if strings.TrimSpace(config.Network) == "" {
		config.Network = "default"
	}
	if strings.TrimSpace(config.VirshPath) == "" {
		config.VirshPath = "virsh"
	}
	if strings.TrimSpace(config.QemuImgPath) == "" {
		config.QemuImgPath = "qemu-img"
	}
	if strings.TrimSpace(config.StorageDir) == "" {
		return nil, errors.New("KVM storage directory is required")
	}
	if strings.TrimSpace(config.BaseImage) == "" {
		return nil, ErrImageRequired
	}
	if config.BaseImageFormat == "" {
		config.BaseImageFormat = "qcow2"
	}
	if config.BaseImageFormat != "qcow2" && config.BaseImageFormat != "raw" {
		return nil, errors.New("unsupported base image format")
	}

	base, err := filepath.Abs(config.BaseImage)
	if err != nil {
		return nil, err
	}
	config.BaseImage = base

	storage, err := filepath.Abs(config.StorageDir)
	if err != nil {
		return nil, err
	}
	config.StorageDir = storage

	return &KVMDriver{config: config, runner: runner}, nil
}

func (d *KVMDriver) Name() string {
	return "kvm-libvirt"
}

func (d *KVMDriver) Capabilities() Capabilities {
	return Capabilities{
		Create: true, Start: true, Stop: true,
		Restart: true, Delete: true, Status: true,
	}
}

func (d *KVMDriver) Create(ctx context.Context, spec Spec) error {
	if err := ValidateSpec(spec); err != nil {
		return err
	}
	if !safeID.MatchString(spec.ID) {
		return fmt.Errorf("%w: unsafe VM ID", ErrInvalidSpec)
	}
	if _, err := os.Stat(d.config.BaseImage); err != nil {
		return fmt.Errorf("base image: %w", err)
	}
	if err := os.MkdirAll(d.config.StorageDir, 0750); err != nil {
		return fmt.Errorf("create VM storage: %w", err)
	}

	disk := filepath.Join(d.config.StorageDir, spec.ID+".qcow2")
	if _, err := os.Stat(disk); err == nil {
		return fmt.Errorf("VM disk already exists: %s", disk)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	_, err := d.runner.Run(ctx, d.config.QemuImgPath,
		"create", "-f", "qcow2",
		"-F", d.config.BaseImageFormat,
		"-b", d.config.BaseImage,
		disk, strconv.FormatInt(int64(spec.Resources.StorageGB), 10)+"G",
	)
	if err != nil {
		return fmt.Errorf("create VM disk: %w", err)
	}

	domainXML, err := d.domainXML(spec, disk)
	if err != nil {
		_ = os.Remove(disk)
		return err
	}

	inputRunner, ok := d.runner.(InputRunner)
	if !ok {
		_ = os.Remove(disk)
		return errors.New("command runner does not support stdin")
	}

	_, err = inputRunner.RunInput(ctx, d.config.VirshPath,
		[]byte(domainXML),
		"--connect", d.config.URI, "define", "/dev/stdin",
	)
	if err != nil {
		_ = os.Remove(disk)
		return fmt.Errorf("define VM: %w", err)
	}
	return nil
}

func (d *KVMDriver) domainXML(spec Spec, disk string) (string, error) {
	name := xmlEscape(spec.ID)
	diskPath := xmlEscape(disk)
	network := xmlEscape(d.config.Network)

	return fmt.Sprintf(`<domain type='kvm'>
  <name>%s</name>
  <memory unit='MiB'>%d</memory>
  <vcpu>%d</vcpu>
  <os><type arch='x86_64'>hvm</type></os>
  <features><acpi/><apic/></features>
  <cpu mode='host-model'/>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source file='%s'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <interface type='network'>
      <source network='%s'/>
      <model type='virtio'/>
    </interface>
    <console type='pty'><target type='serial' port='0'/></console>
    <serial type='pty'><target port='0'/></serial>
    <graphics type='vnc' autoport='yes' listen='127.0.0.1'/>
  </devices>
</domain>`, name, spec.Resources.MemoryMB, spec.Resources.VCPU,
		diskPath, network), nil
}

func xmlEscape(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}

func (d *KVMDriver) Start(ctx context.Context, id string) error {
	return d.virsh(ctx, id, "start")
}

func (d *KVMDriver) Stop(ctx context.Context, id string) error {
	return d.virsh(ctx, id, "shutdown")
}

func (d *KVMDriver) Restart(ctx context.Context, id string) error {
	return d.virsh(ctx, id, "reboot")
}

func (d *KVMDriver) virsh(ctx context.Context, id, operation string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if !safeID.MatchString(id) {
		return ErrInvalidSpec
	}
	_, err := d.runner.Run(ctx, d.config.VirshPath,
		append([]string{"--connect", d.config.URI, operation, id})...,
	)
	return err
}

func (d *KVMDriver) Status(ctx context.Context, id string) (Status, error) {
	if err := ValidateID(id); err != nil {
		return Status{}, err
	}
	if !safeID.MatchString(id) {
		return Status{}, ErrInvalidSpec
	}
	output, err := d.runner.Run(ctx, d.config.VirshPath,
		"--connect", d.config.URI, "domstate", id,
	)
	if err != nil {
		return Status{}, err
	}

	state := StateUnknown
	switch strings.ToLower(strings.TrimSpace(string(output))) {
	case "running", "idle", "in shutdown":
		state = StateRunning
	case "shut off", "shutoff", "crashed":
		state = StateStopped
	}
	return Status{ID: id, State: state}, nil
}

func (d *KVMDriver) Delete(ctx context.Context, id string) error {
	if err := ValidateID(id); err != nil {
		return err
	}
	if !safeID.MatchString(id) {
		return ErrInvalidSpec
	}

	status, err := d.Status(ctx, id)
	if err != nil {
		return err
	}
	if status.State == StateRunning {
		if err := d.virsh(ctx, id, "destroy"); err != nil {
			return err
		}
	}

	if err := d.virsh(ctx, id, "undefine"); err != nil {
		return err
	}

	disk := filepath.Join(d.config.StorageDir, id+".qcow2")
	if err := os.Remove(disk); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove VM disk: %w", err)
	}
	return nil
}
