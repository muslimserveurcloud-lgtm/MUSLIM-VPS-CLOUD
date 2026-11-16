package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

type kvmFakeRunner struct {
	calls    [][]string
	input    []byte
	state    string
	failRun  bool
}

func (f *kvmFakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)

	if f.failRun {
		return nil, os.ErrPermission
	}

	if name == "qemu-img" && len(args) > 7 && args[0] == "create" {
		if err := os.WriteFile(args[7], []byte("fake disk"), 0600); err != nil {
			return nil, err
		}
	}

	for _, arg := range args {
		if arg == "domstate" {
			return []byte(f.state), nil
		}
	}

	return []byte("ok"), nil
}

func (f *kvmFakeRunner) RunInput(ctx context.Context, name string, input []byte, args ...string) ([]byte, error) {
	call := append([]string{name}, args...)
	f.calls = append(f.calls, call)
	f.input = append([]byte(nil), input...)

	if f.failRun {
		return nil, os.ErrPermission
	}

	return []byte("defined"), nil
}

func newKVMTestDriver(t *testing.T, runner *kvmFakeRunner) (*KVMDriver, string) {
	t.Helper()

	dir := t.TempDir()
	base := filepath.Join(dir, "base.qcow2")

	if err := os.WriteFile(base, []byte("base image"), 0600); err != nil {
		t.Fatal(err)
	}

	storage := filepath.Join(dir, "vms")

	driver, err := NewKVMDriver(KVMConfig{
		StorageDir:      storage,
		BaseImage:       base,
		BaseImageFormat: "qcow2",
	}, runner)
	if err != nil {
		t.Fatal(err)
	}

	return driver, storage
}

func testVM() Spec {
	return Spec{
		ID:   "vm-test-01",
		Name: "Test VM",
		Resources: virtual.Resources{
			VCPU:     2,
			MemoryMB: 1024,
			StorageGB: 10,
		},
	}
}

func TestKVMDriverConfiguration(t *testing.T) {
	runner := &kvmFakeRunner{}
	driver, _ := newKVMTestDriver(t, runner)

	if driver.Name() != "kvm-libvirt" {
		t.Fatalf("unexpected driver name: %s", driver.Name())
	}

	caps := driver.Capabilities()
	if !caps.Create || !caps.Start || !caps.Stop ||
		!caps.Restart || !caps.Delete || !caps.Status {
		t.Fatal("expected basic KVM capabilities")
	}

	if caps.Snapshots || caps.Console || caps.PublicIP {
		t.Fatal("unsupported capabilities must remain disabled")
	}
}

func TestKVMCreateDefinesDomainWithXML(t *testing.T) {
	runner := &kvmFakeRunner{}
	driver, storage := newKVMTestDriver(t, runner)

	if err := driver.Create(context.Background(), testVM()); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	disk := filepath.Join(storage, "vm-test-01.qcow2")
	if _, err := os.Stat(disk); err != nil {
		t.Fatalf("VM disk was not created: %v", err)
	}

	xml := string(runner.input)
	for _, expected := range []string{
		"<domain type='kvm'>",
		"<name>vm-test-01</name>",
		"<memory unit='MiB'>1024</memory>",
		"<vcpu>2</vcpu>",
		"<target dev='vda' bus='virtio'/>",
	} {
		if !strings.Contains(xml, expected) {
			t.Errorf("domain XML does not contain %q", expected)
		}
	}

	if len(runner.calls) != 2 {
		t.Fatalf("expected qemu-img and virsh calls, got %d", len(runner.calls))
	}

	last := runner.calls[1]
	if len(last) < 2 || last[len(last)-2] != "define" || last[len(last)-1] != "/dev/stdin" {
		t.Fatalf("unexpected virsh define command: %v", last)
	}
}

func TestKVMCreateRejectsUnsafeID(t *testing.T) {
	runner := &kvmFakeRunner{}
	driver, _ := newKVMTestDriver(t, runner)

	spec := testVM()
	spec.ID = "../outside"

	if err := driver.Create(context.Background(), spec); err == nil {
		t.Fatal("expected unsafe ID to be rejected")
	}
}

func TestKVMCreateRejectsDuplicateDisk(t *testing.T) {
	runner := &kvmFakeRunner{}
	driver, storage := newKVMTestDriver(t, runner)

	if err := os.MkdirAll(storage, 0750); err != nil {
		t.Fatal(err)
	}

	disk := filepath.Join(storage, "vm-test-01.qcow2")
	if err := os.WriteFile(disk, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := driver.Create(context.Background(), testVM()); err == nil {
		t.Fatal("expected duplicate disk to be rejected")
	}
}

func TestKVMInstanceLifecycleCommands(t *testing.T) {
	runner := &kvmFakeRunner{state: "running"}
	driver, storage := newKVMTestDriver(t, runner)

	disk := filepath.Join(storage, "vm-test-01.qcow2")
	if err := os.MkdirAll(storage, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disk, []byte("fake disk"), 0600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	if err := driver.Start(ctx, "vm-test-01"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Stop(ctx, "vm-test-01"); err != nil {
		t.Fatal(err)
	}
	if err := driver.Restart(ctx, "vm-test-01"); err != nil {
		t.Fatal(err)
	}

	status, err := driver.Status(ctx, "vm-test-01")
	if err != nil {
		t.Fatal(err)
	}
	if status.State != StateRunning {
		t.Fatalf("expected running state, got %s", status.State)
	}

	if err := driver.Delete(ctx, "vm-test-01"); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(disk); !os.IsNotExist(err) {
		t.Fatal("VM disk should have been removed")
	}

	operations := []string{"start", "shutdown", "reboot", "domstate", "domstate", "destroy", "undefine"}
	var actual []string

	for _, call := range runner.calls {
		for _, arg := range call {
			if arg == "start" || arg == "shutdown" || arg == "reboot" ||
				arg == "domstate" || arg == "destroy" || arg == "undefine" {
				actual = append(actual, arg)
				break
			}
		}
	}

	if len(actual) != len(operations) {
		t.Fatalf("unexpected lifecycle commands: %v", actual)
	}

	for i := range operations {
		if actual[i] != operations[i] {
			t.Fatalf("command %d: expected %s, got %s", i, operations[i], actual[i])
		}
	}
}
