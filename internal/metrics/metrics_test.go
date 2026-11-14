package metrics

import (
	"fmt"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func httpSeries(t *testing.T) int {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	n := 0
	for _, mf := range families {
		switch mf.GetName() {
		case "piworker_http_requests_total", "piworker_http_request_duration_seconds":
			n += len(mf.GetMetric())
		}
	}
	return n
}

// TestRecordHTTPRequestBoundsMethods: Go accepts any token as a method and
// Prometheus never frees a series, so the method label takes a fixed set
// of values; anything else is "other".
func TestRecordHTTPRequestBoundsMethods(t *testing.T) {
	for _, m := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "other"} {
		RecordHTTPRequest(m, "/bounded", 200, 0.01)
	}
	before := httpSeries(t)
	for i := range 500 {
		RecordHTTPRequest(fmt.Sprintf("M%d", i), "/bounded", 200, 0.01)
		RecordHTTPRequest(fmt.Sprintf("get%d", i), "/bounded", 200, 0.01)
	}
	assert.Equal(t, before, httpSeries(t))
	assert.Equal(t, "GET", methodLabel("GET"))
	assert.Equal(t, "other", methodLabel("get"))
	assert.Equal(t, "other", methodLabel("PROPFIND"))
}
