package engine

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/hypervisor"
	"github.com/muslimserveurcloud-lgtm/MUSLIM-VPS-CLOUD/internal/virtual"
)

type fakeDriver struct {
	calls []string
	fail  string
}

func (f *fakeDriver) Name() string { return "fake-kvm" }
func (f *fakeDriver) Capabilities() hypervisor.Capabilities {
	return hypervisor.Capabilities{Create: true, Start: true, Stop: true, Restart: true, Delete: true, Status: true}
}
func (f *fakeDriver) call(s string) error {
	f.calls = append(f.calls, s)
	if f.fail == s {
		return errors.New("driver failure")
	}
	return nil
}
func (f *fakeDriver) Create(context.Context, hypervisor.Spec) error { return f.call("create") }
func (f *fakeDriver) Start(context.Context, string) error           { return f.call("start") }
func (f *fakeDriver) Stop(context.Context, string) error            { return f.call("stop") }
func (f *fakeDriver) Restart(context.Context, string) error         { return f.call("restart") }
func (f *fakeDriver) Delete(context.Context, string) error          { return f.call("delete") }
func (f *fakeDriver) Status(context.Context, string) (hypervisor.State, error) {
	return hypervisor.StateStopped, nil
}
func TestLifecycleWithDriver(t *testing.T) {
	d := t.TempDir()
	s, e := virtual.NewStore(filepath.Join(d, "alloc.json"), virtual.Resources{VCPU: 4, MemoryMB: 4096, StorageGB: 40})
	if e != nil {
		t.Fatal(e)
	}
	f := &fakeDriver{}
	m, e := NewManagerWithDriver(filepath.Join(d, "instances.json"), s, f)
	if e != nil {
		t.Fatal(e)
	}
	i, e := m.Create("vm", virtual.Resources{VCPU: 1, MemoryMB: 512, StorageGB: 5})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = m.Start(i.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Restart(i.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = m.Stop(i.ID); e != nil {
		t.Fatal(e)
	}
	if e = m.Delete(i.ID); e != nil {
		t.Fatal(e)
	}
	want := []string{"create", "start", "restart", "stop", "delete"}
	if len(f.calls) != len(want) {
		t.Fatalf("calls=%v", f.calls)
	}
	for i := range want {
		if f.calls[i] != want[i] {
			t.Fatalf("calls=%v", f.calls)
		}
	}
}
func TestDriverFailureDoesNotStartInstance(t *testing.T) {
	d := t.TempDir()
	s, _ := virtual.NewStore(filepath.Join(d, "alloc.json"), virtual.Resources{VCPU: 2, MemoryMB: 1024, StorageGB: 10})
	f := &fakeDriver{fail: "create"}
	m, _ := NewManagerWithDriver(filepath.Join(d, "instances.json"), s, f)
	if _, e := m.Create("bad", virtual.Resources{VCPU: 1, MemoryMB: 512, StorageGB: 2}); e == nil {
		t.Fatal("expected error")
	}
	if len(m.List()) != 0 {
		t.Fatal("failed create persisted")
	}
}
