package mux

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func registerBenchRoutes(r *Router, n int) {
	for i := 0; i < n; i++ {
		r.Get(fmt.Sprintf("/s/%d/a/b", i), http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		r.Get(fmt.Sprintf("/p/%d/{id}", i), http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		r.Get(fmt.Sprintf("/w/%d/{*rest}", i), http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	}
}

func benchmarkLookupPath(b *testing.B, n int, path string) {
	r := New()
	registerBenchRoutes(r, n)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rr := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r.ServeHTTP(rr, req)
	}
}

func BenchmarkLookupStatic100(b *testing.B)  { benchmarkLookupPath(b, 100, "/s/42/a/b") }
func BenchmarkLookupStatic1000(b *testing.B) { benchmarkLookupPath(b, 1000, "/s/420/a/b") }
func BenchmarkLookupParam1000(b *testing.B)  { benchmarkLookupPath(b, 1000, "/p/420/abc") }
func BenchmarkLookupWildcard1000(b *testing.B) {
	benchmarkLookupPath(b, 1000, "/w/420/x/y/z")
}
func BenchmarkLookupMiss1000(b *testing.B) { benchmarkLookupPath(b, 1000, "/does/not/exist") }

func BenchmarkUsePrefixRewrap1000(b *testing.B) {
	for i := 0; i < b.N; i++ {
		r := New()
		registerBenchRoutes(r, 1000)
		r.UsePrefix("/s/42", func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				next.ServeHTTP(w, req)
			})
		})
	}
}
