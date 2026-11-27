package cli_test

import (
	"path/filepath"
	"testing"
)

func TestCheckDurationsAcceptsReportWithCPUFields(t *testing.T) {
	root := durationsRepo(t)
	hint := filepath.Join(t.TempDir(), "cpu-report.json")
	mustWrite(t, hint, `{"durations":{"has-widget":2,"no-banana":1},"cpu_durations":{"has-widget":1,"no-banana":0},"cpu_total":3}`)
	code, out, errOut := runCLI(t, "check", "-C", root, "--durations", hint)
	if code != 0 {
		t.Fatalf("CPU fields broke the forward-compatible durations reader: exit %d\nstdout:\n%s\nstderr:\n%s", code, out, errOut)
	}
}
