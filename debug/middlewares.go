package debug

import (
	"mekapi/trc/eztrc"
	"net/http"
	"strconv"
	"time"

	"zenith/metrics"

	"github.com/NYTimes/gziphandler"
	"github.com/gorilla/mux"
)

func GZipMiddleware(next http.Handler) http.Handler {
	return gziphandler.GzipHandler(next)
}

func TracingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, finish := eztrc.Create(r.Context(), getRouteName(r))
		defer finish()

		eztrc.Tracef(ctx, "%s %s %s", r.RemoteAddr, r.Method, r.URL.String())

		for k, vs := range r.Header {
			for _, v := range vs {
				eztrc.Tracef(ctx, "→ %s: %s", k, v)
			}
		}

		iw := newInterceptor(w)
		defer func(b time.Time) {
			code := iw.Code()
			sent := iw.Written()
			took := time.Since(b).Truncate(time.Microsecond)
			eztrc.Tracef(ctx, "HTTP %d, %dB, %s", code, sent, took)
		}(time.Now())

		next.ServeHTTP(iw, r.WithContext(ctx))
	})
}

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		iw := newInterceptor(w)
		defer func(b time.Time) {
			route := getRouteName(r)
			code := strconv.Itoa(iw.Code())
			sec := time.Since(b).Seconds()
			metrics.HTTPRequestDurationSeconds.WithLabelValues(route, code).Observe(sec)
		}(time.Now())

		next.ServeHTTP(iw, r)
	})
}

// getRouteName only works if it's called via mux.Router.Use(middleware).
// If you try to decorate an http.Handler, it won't identify the route.
func getRouteName(r *http.Request) string {
	if route := mux.CurrentRoute(r); route != nil {
		// If an explicit name was defined, use that directly.
		if name := route.GetName(); name != "" {
			return name
		}

		// If we can get a path template, use that, prefixed by the HTTP verb.
		if pathtpl, _ := route.GetPathTemplate(); pathtpl != "" {
			return r.Method + " " + pathtpl
		}
	}

	// Fallback: method and path.
	return r.Method + " " + r.URL.Path
}

//
//
//

type interceptor struct {
	http.ResponseWriter

	code int
	n    int
}

func newInterceptor(w http.ResponseWriter) *interceptor {
	return &interceptor{ResponseWriter: w}
}

func (i *interceptor) WriteHeader(code int) {
	if i.code == 0 {
		i.code = code
	}
	i.ResponseWriter.WriteHeader(code)
}

func (i *interceptor) Write(p []byte) (int, error) {
	n, err := i.ResponseWriter.Write(p)
	i.n += n
	return n, err
}

func (i *interceptor) Code() int {
	if i.code == 0 {
		return http.StatusOK
	}
	return i.code
}

func (i *interceptor) Written() int {
	return i.n
}
