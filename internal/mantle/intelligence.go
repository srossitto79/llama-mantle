package mantle

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

// The companion owns benchmark execution and storage. Keep its HTTP interface
// behind Mantle's existing API entry point, including streaming responses.
func (h *Handler) registerIntelligenceRoutes(mux *http.ServeMux) {
	endpoint := strings.TrimSpace(os.Getenv("LLAMA_INTELLIGENCE_URL"))
	target, err := url.Parse(endpoint)
	var handler http.Handler
	if err != nil || target == nil || target.Host == "" || (target.Scheme != "http" && target.Scheme != "https") || target.RawQuery != "" || target.Fragment != "" {
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			jsonError(w, http.StatusServiceUnavailable, "Intelligence is not configured. Set LLAMA_INTELLIGENCE_URL to the benchmark service URL and restart Mantle.")
		})
	} else {
		proxy := httputil.NewSingleHostReverseProxy(target)
		director := proxy.Director
		proxy.Director = func(r *http.Request) {
			r.URL.Path = "/api/" + strings.TrimPrefix(r.URL.Path, "/api/mantle/studio/intelligence/")
			r.URL.RawPath = ""
			director(r)
			r.Host = target.Host
			// The companion does not need the user's Mantle credentials.
			r.Header.Del("Authorization")
			r.Header.Del("Cookie")
			r.Header.Del("X-Api-Key")
		}
		proxy.FlushInterval = -1
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			jsonError(w, http.StatusBadGateway, "The Intelligence service is unavailable. Check that the companion container is running.")
		}
		handler = proxy
	}
	for _, route := range []string{
		"GET config", "GET results", "GET run/status", "GET run/stream",
		"POST run", "POST run/stop", "GET runs", "GET runs/{id}", "DELETE runs/{id}",
		"GET download/status", "POST download/polyglot", "POST download/swebench",
	} {
		method, path, _ := strings.Cut(route, " ")
		mux.Handle(method+" /api/mantle/studio/intelligence/"+path, handler)
	}
}
