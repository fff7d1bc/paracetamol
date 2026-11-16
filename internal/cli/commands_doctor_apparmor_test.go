package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"paracetamol/internal/ui"
)

func TestDoctorAppArmorPolicyIsInformational(t *testing.T) {
	for _, test := range []struct {
		name     string
		contents string
		err      error
		want     string
	}{
		{name: "absent", err: &os.PathError{Op: "open", Path: "sysctl", Err: os.ErrNotExist}},
		{name: "off", contents: "0\n", want: "user namespace restriction is off"},
		{name: "on", contents: "1\n", want: "restricts unprivileged user namespaces"},
		{name: "whitespace", contents: " \t1\n", want: "restricts unprivileged user namespaces"},
		{name: "unreadable", err: os.ErrPermission, want: "user namespace restriction state is unreadable"},
		{name: "read failure with contents", contents: "1", err: os.ErrPermission, want: "user namespace restriction state is unreadable"},
		{name: "empty", want: "user namespace restriction state is invalid"},
		{name: "invalid", contents: "2\n", want: "user namespace restriction state is invalid"},
		{name: "multiple values", contents: "0\n1\n", want: "user namespace restriction state is invalid"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			reportAppArmorPolicyResult(&output, ui.New(&output, nil), []byte(test.contents), test.err)
			want := ""
			if test.want != "" {
				want = "  AppArmor       " + test.want + "\n"
			}
			if got := output.String(); got != want {
				t.Fatalf("AppArmor report = %q, want %q", got, want)
			}
			for _, forbidden := range []string{"bubblewrap", "Host action", "Apply", "sudo", "sysctl", "/etc/"} {
				if strings.Contains(output.String(), forbidden) {
					t.Errorf("AppArmor report contains obsolete remediation %q", forbidden)
				}
			}
		})
	}
}
