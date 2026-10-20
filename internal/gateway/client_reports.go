package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrRequestNotRetained does not distinguish unknown requests from expired
// reports, another gateway instance, or requests outside the active cache.
var ErrRequestNotRetained = errors.New("request report is not retained; it may be unknown, expired, from another gateway instance, or outside the bounded active cache")

// FetchRequestReport reads one bounded metadata snapshot. It never retries,
// waits for completion, or contacts a private inference backend.
func FetchRequestReport(ctx context.Context, endpoint, key, id string) (RequestReport, error) {
	if !validRequestReportID(id) {
		return RequestReport{}, fmt.Errorf("request report requires one canonical request ID")
	}
	raw, err := fetchMetadata(ctx, endpoint, key, requestReportPath+id, nil)
	if err != nil {
		var httpError *metadataHTTPError
		if errors.As(err, &httpError) && httpError.StatusCode == http.StatusNotFound {
			return RequestReport{}, ErrRequestNotRetained
		}
		return RequestReport{}, err
	}
	var report RequestReport
	if err := json.Unmarshal(raw, &report); err != nil {
		return RequestReport{}, fmt.Errorf("cannot decode gateway request report")
	}
	if report.Schema != RequestReportSchema {
		return RequestReport{}, fmt.Errorf("gateway returned unsupported request report schema")
	}
	// A report must belong to both the requested process epoch and the exact
	// request. A valid gateway marker alone cannot establish that correlation.
	if report.InstanceID != id[1:33] {
		return RequestReport{}, fmt.Errorf("gateway returned request report for a different instance")
	}
	switch report.State {
	case "active":
		if report.Active == nil || report.Record != nil || report.Active.ID != id {
			return RequestReport{}, fmt.Errorf("gateway returned an inconsistent active request report")
		}
		switch report.Phase {
		case "receiving", "waiting", "upstream":
		default:
			return RequestReport{}, fmt.Errorf("gateway returned an invalid active request phase")
		}
	case "completed":
		if report.Record == nil || report.Active != nil || report.Record.ID != id || report.Phase != "" {
			return RequestReport{}, fmt.Errorf("gateway returned an inconsistent completed request report")
		}
	default:
		return RequestReport{}, fmt.Errorf("gateway returned an invalid request report state")
	}
	return report, nil
}
