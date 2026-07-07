package yagonode

import (
	"net/http"
	"net/http/pprof"
)

// pprofPrefix mounts Go's runtime profiles on the ops listener. The whole ops
// mux sits behind the admin guard, so CPU, heap, and goroutine profiles are
// operator-only — continuous-profiling collectors (Parca, Pyroscope) scrape
// these endpoints with an authenticated session or a reverse-proxy exemption
// (OPS-08).
const pprofPrefix = "/debug/pprof/"

// mountProfiling registers the pprof index, the named profiles, and the
// delta endpoints net/http/pprof serves specially.
func mountProfiling(mux *http.ServeMux) {
	mux.HandleFunc(pprofPrefix, pprof.Index)
	mux.HandleFunc(pprofPrefix+"cmdline", pprof.Cmdline)
	mux.HandleFunc(pprofPrefix+"profile", pprof.Profile)
	mux.HandleFunc(pprofPrefix+"symbol", pprof.Symbol)
	mux.HandleFunc(pprofPrefix+"trace", pprof.Trace)
}
