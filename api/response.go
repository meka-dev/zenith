package api

import (
	"encoding/json"
	"errors"
	"mekapi/trc/eztrc"
	"net/http"

	"zenith/block"
	"zenith/chain"
	"zenith/store"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
)

func respondOK(w http.ResponseWriter, r *http.Request, response any) {
	w.Header().Set("content-type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		eztrc.Errorf(r.Context(), "write response: %v", err)
	}
}

func respondError(w http.ResponseWriter, r *http.Request, err error, fallbackCode int, logger log.Logger) {
	code, trueError := classifyError(err, fallbackCode)

	if trueError {
		eztrc.Errorf(r.Context(), "error: %v (%d)", err, code)
		level.Error(logger).Log("remote_addr", r.RemoteAddr, "method", r.Method, "path", r.URL.Path, "err", err, "code", code)
	} else {
		eztrc.Tracef(r.Context(), "quasi-error: %v (%d)", err, code)
	}

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(errorResponse{
		Error:      err.Error(),
		StatusCode: code,
		StatusText: http.StatusText(code),
	}); err != nil {
		eztrc.Errorf(r.Context(), "write response: %v", err)
	}
}

func classifyError(err error, fallback int) (int, bool) {
	switch {
	case err == nil:
		return http.StatusOK, false
	case errors.Is(err, block.ErrAuctionFinished):
		return http.StatusGone, false
	case errors.Is(err, block.ErrAuctionTooNew):
		return http.StatusTooEarly, false
	case errors.Is(err, block.ErrAuctionTooOld):
		return http.StatusGone, false
	case errors.Is(err, block.ErrAuctionUnavailable):
		return http.StatusExpectationFailed, false
	case errors.Is(err, chain.ErrBadSignature):
		return http.StatusUnauthorized, true
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound, true
	case errors.Is(err, block.ErrInvalidRequest):
		return http.StatusBadRequest, true
	default:
		return fallback, true
	}
}

type errorResponse struct {
	Error      string `json:"error"`
	StatusCode int    `json:"status_code"`
	StatusText string `json:"status_text"`
}
