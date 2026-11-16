package hypervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

type recordingRunner struct{ calls [][]string }

func (r *recordingRunner) Run(_ context.Context, n string, a ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{n}, a...))
	if n == "qemu-img" && len(a) > 0 {
		_ = os.WriteFile(a[len(a)-1], []byte("fake-disk"), 0600)
	}
	return []byte("shut off"), nil
}
func TestKVMDriverCreateAndLifecycleCommands(t *testing.T) {
	d := t.TempDir()
	base := filepath.Join(d, "base.qcow2")
	if err := os.WriteFile(base, []byte("fake"), 0600); err != nil {
		t.Fatal(err)
	}
	r := &recordingRunner{}
	driver, err := NewKVMDriver(KVMConfig{StorageDir: filepath.Join(d, "disks"), BaseImage: base}, r)
	if err != nil {
		t.Fatal(err)
	}
	spec := Spec{ID: "vm-test", Name: "vm", Resources: virtual.Resources{VCPU: 1, MemoryMB: 512, StorageGB: 5}}
	if err := driver.Create(context.Background(), spec); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(d, "disks", "vm-test.qcow2")); err != nil {
		t.Fatal(err)
	}
	if err := driver.Start(context.Background(), spec.ID); err != nil {
		t.Fatal(err)
	}
	if len(r.calls) < 3 {
		t.Fatalf("expected qemu-img and virsh calls, got %v", r.calls)
	}
}
