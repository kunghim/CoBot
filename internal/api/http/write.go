// Package http exposes the public /api/v1 surface and the internal helper
// routes. Handlers validate input, resolve identity and call app methods;
// they never run SQL or engine RPC directly.
package http

import (
	"encoding/json"
	"net/http"

	"github.com/kunghim/CoBot/internal/domain"
)

type okEnvelope struct {
	Data      any    `json:"data"`
	RequestID string `json:"requestId"`
}

type errorBody struct {
	Code       domain.Code    `json:"code"`
	MessageKey string         `json:"messageKey"`
	Params     map[string]any `json:"params,omitempty"`
}

type errorEnvelope struct {
	Error     errorBody `json:"error"`
	RequestID string    `json:"requestId"`
}

func writeData(w http.ResponseWriter, r *http.Request, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(okEnvelope{Data: data, RequestID: requestIDOf(r)})
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	de := domain.AsError(err)
	if de.Code == domain.CodeInternal {
		recordFailure(r, err)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(de.HTTPStatus())
	_ = json.NewEncoder(w).Encode(errorEnvelope{
		Error:     errorBody{Code: de.Code, MessageKey: de.MessageKey, Params: de.Params},
		RequestID: requestIDOf(r),
	})
}

// decodeBody reads a JSON body with a hard size limit and rejects unknown
// fields so typos surface as 400 instead of being ignored.
func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return domain.InvalidRequest("error.invalid_json", map[string]any{"detail": err.Error()})
	}
	return nil
}
