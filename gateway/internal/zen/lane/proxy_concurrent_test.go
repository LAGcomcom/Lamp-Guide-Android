package lane

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

func TestProxySwapKeepsConcurrentRequestsUsable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	defer srv.Close()
	transport := &proxyTransport{transport: http.DefaultTransport.(*http.Transport).Clone()}
	client := &http.Client{Transport: transport}
	defer transport.CloseIdleConnections()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 30 {
			transport.swap(http.DefaultTransport.(*http.Transport).Clone())
		}
	}()
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				resp, err := client.Get(srv.URL)
				if err != nil {
					t.Error(err)
					return
				}
				raw, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || string(raw) != "ok" {
					t.Errorf("response = %q, %v", raw, err)
				}
			}
		}()
	}
	wg.Wait()
}
