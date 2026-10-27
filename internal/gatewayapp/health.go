package gatewayapp

import (
	"net/http"
	"sync/atomic"
)

type State struct {
	ready    atomic.Bool
	draining atomic.Bool
}

func NewState() *State {
	return &State{}
}

func (s *State) MarkReady() {
	s.ready.Store(true)
}
func (s *State) MarkDraining() {
	s.draining.Store(true)
}
func (s *State) Ready() bool {
	return s.ready.Load() && !s.draining.Load()
}

func (s *State) Healthz(w http.ResponseWriter, r *http.Request) {
	writeProbe(w, http.StatusOK, `{"status":"ok"}`)
}

func (s *State) Readyz(w http.ResponseWriter, r *http.Request) {
	if !s.Ready() {
		writeProbe(w, http.StatusServiceUnavailable, `{"status":"draining"}`)
		return
	}
	writeProbe(w, http.StatusOK, `{"status":"ok"}`)
}

// 两个探针的写法完全一样，抽出来避免以后改漏其中一处。
func writeProbe(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json") // 必须在 WriteHeader 之前
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}
