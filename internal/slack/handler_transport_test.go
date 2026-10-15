package slackclient

import (
	"net/http"
	"net/http/httptest"
)

// handlerTransport serves each request by calling h in memory, with no
// socket. A test that runs inside a synctest bubble can then use a
// fake clock for every timeout on the way, since nothing waits on real
// I/O. As a real transport does, it returns the request's ctx error as
// soon as that ctx ends, even if h is still running.
type handlerTransport struct{ h http.Handler }

func (t handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		t.h.ServeHTTP(rec, r)
	}()
	select {
	case <-done:
		return rec.Result(), nil
	case <-r.Context().Done():
		return nil, r.Context().Err()
	}
}

// handlerClient is an http.Client whose requests go to h in memory.
func handlerClient(h http.Handler) *http.Client {
	return &http.Client{Transport: handlerTransport{h}}
}
