package hostdoctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTTMStateAndMemoryTargets(t *testing.T) {
	root := t.TempDir()
	parameters := filepath.Join(root, "ttm", "parameters")
	if err := os.MkdirAll(parameters, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parameters, "pages_limit"), []byte("29360128\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parameters, "page_pool_size"), []byte("29360128\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	state := ReadTTMState(root)
	if state == nil || state.Module != "ttm" || !state.PagePoolKnown {
		t.Fatalf("state=%#v", state)
	}
	if target, ok := TargetGiB(128 << 30); !ok || target != 112 {
		t.Fatalf("target=%d ok=%t", target, ok)
	}
	if !MemoryReady(state, 112<<30, true, 112) {
		t.Fatal("accepted shared-memory state is not ready")
	}
}

func TestReadTTMStatePreservesKnownZeroPagePool(t *testing.T) {
	root := t.TempDir()
	parameters := filepath.Join(root, "ttm", "parameters")
	if err := os.MkdirAll(parameters, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]string{"pages_limit": "1\n", "page_pool_size": "0\n"} {
		if err := os.WriteFile(filepath.Join(parameters, name), []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := ReadTTMState(root)
	if state == nil || !state.PagePoolKnown || state.PagePoolSize != 0 {
		t.Fatalf("state=%#v", state)
	}
}

func TestRemediationSelectsHostBootMechanism(t *testing.T) {
	state := &TTMState{Module: "ttm", PagesLimit: 16777216, PagePoolSize: 16777216, PagePoolKnown: true}
	ostree := strings.Join(Remediation(state, 112, BootTools{OstreeBooted: true, RPMOstree: true}, "paracetamol"), "\n")
	if !strings.Contains(ostree, "rpm-ostree kargs") || !strings.Contains(ostree, "amdgpu.gttsize=114688") {
		t.Fatalf("ostree=%s", ostree)
	}
	grub := strings.Join(Remediation(state, 112, BootTools{GRUBDropIn: true, UpdateGRUB: true}, "paracetamol"), "\n")
	if !strings.Contains(grub, "/etc/default/grub.d/70-paracetamol-ttm.cfg") || !strings.Contains(grub, "sudo update-grub") {
		t.Fatalf("grub=%s", grub)
	}
	grubby := strings.Join(Remediation(state, 112, BootTools{Grubby: true}, "paracetamol"), "\n")
	if !strings.Contains(grubby, "grubby --update-kernel=ALL") || !strings.Contains(grubby, "--remove-args='amdgpu.gttsize ttm.pages_limit ttm.page_pool_size'") {
		t.Fatalf("grubby=%s", grubby)
	}
}

func TestStrixHaloKernelBaseline(t *testing.T) {
	if warning := StrixHaloKFDWarning("6.18.3-200.fc44.x86_64"); !strings.Contains(warning, "upstream 6.18.4+") {
		t.Fatalf("warning=%q", warning)
	}
	if warning := StrixHaloKFDWarning("6.18.4-generic"); warning != "" {
		t.Fatalf("warning=%q", warning)
	}
}
