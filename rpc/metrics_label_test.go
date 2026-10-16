package rpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/tolelom/tolchain/metrics"
)

func TestServer_UnknownMethodsShareOneMetricSeries(t *testing.T) {
	s := newTestServer("")
	defer s.limiter.stop()
	before := countSeries(metrics.RPCRequests)

	for i := 0; i < 50; i++ {
		body, _ := json.Marshal(Request{JSONRPC: "2.0", ID: 1, Method: fmt.Sprintf("junk-%d", i)})
		s.serveHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body)))
	}

	if grown := countSeries(metrics.RPCRequests) - before; grown > 1 {
		t.Fatalf("50 unknown method names created %d request series; want at most 1", grown)
	}
}

func countSeries(c prometheus.Collector) int {
	ch := make(chan prometheus.Metric)
	go func() {
		c.Collect(ch)
		close(ch)
	}()
	n := 0
	for range ch {
		n++
	}
	return n
}
