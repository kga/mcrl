package cli

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kga/mcrl/internal/httpx"
	"github.com/kga/mcrl/internal/mackerel"
)

// TestLatestMetricTimesConcurrency checks that the time-series lookups run
// at most the asked number of requests at once.
func TestLatestMetricTimesConcurrency(t *testing.T) {
	for _, n := range []int{1, 2} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			var inFlight, peak atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v0/tsdb/latest" {
					nulls := `{"loadavg5":null,"cpu.user.percentage":null,"memory.used":null}`
					fmt.Fprintf(w, `{"tsdbLatest":{"h1":%[1]s,"h2":%[1]s,"h3":%[1]s,"h4":%[1]s,"h5":%[1]s,"h6":%[1]s}}`, nulls)
					return
				}
				cur := inFlight.Add(1)
				defer inFlight.Add(-1)
				for {
					old := peak.Load()
					if cur <= old || peak.CompareAndSwap(old, cur) {
						break
					}
				}
				time.Sleep(20 * time.Millisecond)
				if !strings.HasSuffix(r.URL.Path, "/metrics") {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				fmt.Fprint(w, `{"metrics":[{"time":100,"value":1}]}`)
			}))
			defer srv.Close()
			u, _ := url.Parse(srv.URL)
			c := mackerel.New(&httpx.Client{BaseURL: u, APIKey: "k"})
			ids := []string{"h1", "h2", "h3", "h4", "h5", "h6"}
			latest, _, err := latestMetricTimes(t.Context(), c, ids, func(string) (int64, int64) { return 0, 1000 }, n)
			if err != nil {
				t.Fatal(err)
			}
			if len(latest) != len(ids) {
				t.Errorf("got %d hosts, want %d", len(latest), len(ids))
			}
			if got := int(peak.Load()); got != n {
				t.Errorf("peak requests at once = %d, want %d", got, n)
			}
		})
	}
}
