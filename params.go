package mux

import (
	"maps"
	"net/http"
)

type paramsKey struct{}

// Param returns the named URL parameter from the request's context.
func Param(r *http.Request, name string) string {
	if m, ok := routeParamsFromContext(r); ok {
		return m[name]
	}

	return ""
}

// Params returns a copy of all URL parameters present on the request.
func Params(r *http.Request) map[string]string {
	out := map[string]string{}
	if m, ok := routeParamsFromContext(r); ok {
		maps.Copy(out, m)
	}

	return out
}

func routeParamsFromContext(r *http.Request) (map[string]string, bool) {
	m, ok := r.Context().Value(paramsKey{}).(map[string]string)
	if !ok || len(m) == 0 {
		return nil, false
	}

	return m, true
}
