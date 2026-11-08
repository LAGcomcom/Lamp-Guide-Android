package gateway

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegressionAdminFence(t *testing.T) {
	cases := []struct{ name, host, origin string }{
		{"non-loopback-host-without-port", "review.invalid", ""},
		{"non-loopback-ipv6", "[2001:db8::1]:8787", ""},
		{"origin-suffix", "127.0.0.1:8787", "http://localhost.review.invalid"},
		{"origin-wrong-port", "127.0.0.1:8787", "http://localhost:9999"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://127.0.0.1:8787/admin/api/settings", strings.NewReader("{}"))
			r.Host = tc.host
			r.RemoteAddr = "127.0.0.1:54321"
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			w := httptest.NewRecorder()
			if adminAllowed(w, r, true) {
				t.Fatalf("accepted host=%q origin=%q", tc.host, tc.origin)
			}
		})
	}
}
