package cli

import "strconv"

// checkpointDiagnostic encodes only persisted diagnostics, not the original
// error. Joined errors and subprocess stderr can contain terminal controls or
// multiple lines, while checkpoint reasons must remain printable single lines.
// Quoting preserves every cause and distinguishes literal backslashes from
// escaped controls without relaxing validation of paths or other metadata.
func checkpointDiagnostic(err error) string {
	if err == nil {
		return ""
	}
	quoted := strconv.Quote(err.Error())
	if quoted == `""` {
		// A non-nil empty error must still be distinguishable from no failure.
		return quoted
	}
	return quoted[1 : len(quoted)-1]
}
