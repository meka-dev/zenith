package debug

import (
	"fmt"
	"mekapi/trc/eztrc"
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func NewHandler() http.Handler {
	router := mux.NewRouter()
	router.StrictSlash(true)

	router.Methods("GET").Path("/debug/pprof/").HandlerFunc(pprof.Index)
	router.Methods("GET").Path("/debug/pprof/cmdline").HandlerFunc(pprof.Cmdline)
	router.Methods("GET").Path("/debug/pprof/profile").HandlerFunc(pprof.Profile)
	router.Methods("GET").Path("/debug/pprof/symbol").HandlerFunc(pprof.Symbol)
	router.Methods("GET").Path("/debug/pprof/trace").HandlerFunc(pprof.Trace)
	router.Methods("GET").Path("/debug/pprof/goroutine").Handler(pprof.Handler("goroutine"))
	router.Methods("GET").Path("/debug/pprof/threadcreate").Handler(pprof.Handler("threadcreate"))
	router.Methods("GET").Path("/debug/pprof/heap").Handler(pprof.Handler("heap"))
	router.Methods("GET").Path("/debug/pprof/allocs").Handler(pprof.Handler("allocs"))
	router.Methods("GET").Path("/debug/pprof/block").Handler(pprof.Handler("block"))
	router.Methods("GET").Path("/debug/pprof/mutex").Handler(pprof.Handler("mutex"))

	router.Methods("GET").Path("/metrics").Handler(promhttp.Handler())
	router.Methods("GET").Path("/traces").Handler(eztrc.TracesHandler)
	router.Methods("GET").Path("/logs").Handler(eztrc.LogsHandler)

	router.Methods("GET").Path("/").Handler(indexHandler(router))

	router.Use(
		TracingMiddleware,
		// MetricsMiddleware, // debug endpoint metrics just pollute the dashboards
		GZipMiddleware,
	)

	return router
}

func indexHandler(r *mux.Router) http.Handler {
	type endpointSet struct {
		name      string
		endpoints []string
	}
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var (
			debug   = endpointSet{name: "debug"}
			mekatek = endpointSet{name: "mekatek"}
		)
		r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
			var (
				routePath, _ = route.GetPathTemplate()
				isValid      = routePath != ""
				isDebug      = strings.HasPrefix(routePath, "/debug/")
				isIndex      = routePath == "/"
				addToDebug   = isValid && isDebug
				addToMekatek = isValid && !isDebug && !isIndex
			)
			switch {
			case addToDebug:
				debug.endpoints = append(debug.endpoints, routePath)
			case addToMekatek:
				mekatek.endpoints = append(mekatek.endpoints, routePath)
			}
			return nil
		})

		w.Header().Set("content-type", "text/html; charset=utf-8")

		for _, endpoints := range []endpointSet{debug, mekatek} {
			fmt.Fprintf(w, "<h1>%s</h1>\n", endpoints.name)
			fmt.Fprintf(w, "<ul>\n")
			for _, endpoint := range endpoints.endpoints {
				fmt.Fprintf(w, "<li><a href=\"%[1]s\">%[1]s</a></li>\n", endpoint)
			}
			fmt.Fprintf(w, "</ul>\n")
		}
	})
}
