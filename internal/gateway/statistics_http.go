package gateway

import (
	"fmt"
	"net/http"
	"net/url"
)

func statisticsDays(rawQuery string) (int, error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return 0, fmt.Errorf("statistics query is invalid")
	}
	for key, items := range values {
		if key != "days" || len(items) != 1 {
			return 0, fmt.Errorf("only one days parameter is supported")
		}
	}
	if len(values) == 0 {
		return 7, nil
	}
	switch values.Get("days") {
	case "1":
		return 1, nil
	case "7":
		return 7, nil
	case "30":
		return 30, nil
	default:
		return 0, fmt.Errorf("days must be 1, 7, or 30")
	}
}

func (server *Server) statistics(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Cache-Control", "no-store")
	days, err := statisticsDays(request.URL.RawQuery)
	if err != nil {
		writeAPIError(writer, http.StatusBadRequest, "invalid_request_error", "invalid_days", err.Error())
		return
	}
	snapshot, err := server.stats.Snapshot(request.Context(), days)
	if err != nil {
		writeAPIError(writer, http.StatusServiceUnavailable, "gateway_error", "statistics_unavailable", "Gateway statistics are unavailable")
		return
	}
	writeJSON(writer, http.StatusOK, snapshot)
}
