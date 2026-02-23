package machcheck

import (
	"context"
	"testing"

	"codex-runner/internal/codexremote/config"
)

func TestCheckAddrOnlyHealthy(t *testing.T) {
	orig := addrHealthCheck
	t.Cleanup(func() { addrHealthCheck = orig })
	addrHealthCheck = func(ctx context.Context, addr string) bool { return true }

	st := Check(context.Background(), config.Machine{
		Name:       "addr-only",
		Addr:       "http://127.0.0.1:7337",
		DaemonPort: 7337,
	})

	if st.DaemonOK != true {
		t.Fatalf("DaemonOK = %v, want true", st.DaemonOK)
	}
	if st.SSHOK != false {
		t.Fatalf("SSHOK = %v, want false", st.SSHOK)
	}
	if st.Error != "" {
		t.Fatalf("Error = %q, want empty", st.Error)
	}
	if st.DaemonAddr != "http://127.0.0.1:7337" {
		t.Fatalf("DaemonAddr = %q, want %q", st.DaemonAddr, "http://127.0.0.1:7337")
	}
}

func TestCheckRequiresSSHOrAddr(t *testing.T) {
	st := Check(context.Background(), config.Machine{Name: "invalid", DaemonPort: 7337})
	if st.DaemonOK {
		t.Fatalf("DaemonOK = true, want false")
	}
	if st.SSHOK {
		t.Fatalf("SSHOK = true, want false")
	}
	if st.Error != "machine.ssh or machine.addr is required for check" {
		t.Fatalf("Error = %q", st.Error)
	}
}
