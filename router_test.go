package mux

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStaticRoute(t *testing.T) {
	r := New()
	r.Get("/hello", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/hello")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	if string(body) != "ok" {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestParamRoute(t *testing.T) {
	r := New()
	r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(Param(r, "id")))
	}))
	ts := httptest.NewServer(r)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/users/123")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "123" {
		t.Fatalf("expected 123 got %s", string(b))
	}
}

func TestDuplicateParamNames(t *testing.T) {
	r := New()
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic on duplicate param names during registration")
		}
	}()
	// duplicate param name {id} used twice
	r.Get("/items/{id}/sub/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
}

func TestEmptySegmentPattern(t *testing.T) {
	r := New()
	// With path normalization, double slashes are collapsed.
	// This should not panic; the pattern is normalized to "/bad/path".
	r.Get("/bad//path", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
}

func TestHandleMultipleMethodsSameHandler(t *testing.T) {
	r := New()
	r.HandleMethods([]string{http.MethodGet, http.MethodPost}, "/multi", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(r.Method))
	}))
	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/multi")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != http.MethodGet {
		t.Fatalf("expected GET got %s", string(b))
	}

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/multi", nil)
	res2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b2, _ := io.ReadAll(res2.Body)
	if string(b2) != http.MethodPost {
		t.Fatalf("expected POST got %s", string(b2))
	}
}

func TestWildcard(t *testing.T) {
	r := New()
	r.Get("/static/{*path}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(Param(r, "path")))
	}))
	ts := httptest.NewServer(r)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/static/images/2023/a.png")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "images/2023/a.png" {
		t.Fatalf("unexpected wildcard %s", string(b))
	}
}

func TestPrecedenceStaticOverParam(t *testing.T) {
	r := New()
	r.Get("/a/{x}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("param"))
	}))
	r.Get("/a/b", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("static"))
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/a/b")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "static" {
		t.Fatalf("expected static handler, got %q", string(b))
	}
}

func TestPrecedenceParamOverWildcard(t *testing.T) {
	r := New()
	r.Get("/a/{*rest}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("wild"))
	}))
	r.Get("/a/{x}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("param"))
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/a/b")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	if string(b) != "param" {
		t.Fatalf("expected param handler, got %q", string(b))
	}
}

func TestMethodNotAllowed(t *testing.T) {
	r := New()
	r.Get("/resource", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("got"))
	}))
	ts := httptest.NewServer(r)
	defer ts.Close()
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/resource", nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 got %d", res.StatusCode)
	}
	if allow := res.Header.Get("Allow"); allow == "" {
		t.Fatalf("Allow header missing")
	}
}

func TestGroupAndMiddleware(t *testing.T) {
	r := New()
	// middleware to set header
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-M", "1")
			next.ServeHTTP(w, r)
		})
	})
	r.Group("/api/v1", func(sub *Router) {
		sub.Get("/ping", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("pong"))
		}))
	})
	ts := httptest.NewServer(r)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/api/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("X-M") != "1" {
		t.Fatalf("middleware not applied")
	}
}

func TestUsePrefixAppliesToExistingAndNew(t *testing.T) {
	r := New()
	// prefix middleware to set header
	r.UsePrefix("/v2", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-P", "1")
			next.ServeHTTP(w, r)
		})
	})

	// existing route before UsePrefix would have been added, but we called UsePrefix first
	r.Get("/v2/foo", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/v2/foo")
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("X-P") != "1" {
		t.Fatalf("prefix middleware not applied to new route")
	}

	// Now add a new prefix middleware and ensure it applies to existing route
	r.UsePrefix("/v2", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("X-P2", "1")
			next.ServeHTTP(w, r)
		})
	})

	res2, err := http.Get(ts.URL + "/v2/foo")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Header.Get("X-P2") != "1" {
		t.Fatalf("second prefix middleware not applied to existing route")
	}
}

func TestUsePrefixOrderPrefixThenUseThenHandler(t *testing.T) {
	r := New()
	order := make([]string, 0, 3)

	r.UsePrefix("/v2", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "prefix")
			next.ServeHTTP(w, req)
		})
	})
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			order = append(order, "use")
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/v2/a", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		order = append(order, "handler")
		w.WriteHeader(http.StatusOK)
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/v2/a")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()

	got := strings.Join(order, ",")
	if got != "prefix,use,handler" {
		t.Fatalf("unexpected order: %s", got)
	}
}

func TestUsePrefixSegmentAware(t *testing.T) {
	r := New()
	r.UsePrefix("/v2", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Add("X-P", "1")
			next.ServeHTTP(w, req)
		})
	})

	r.Get("/v21/a", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r.Get("/v2/a", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()

	res1, err := http.Get(ts.URL + "/v21/a")
	if err != nil {
		t.Fatal(err)
	}
	if res1.Header.Get("X-P") != "" {
		t.Fatalf("prefix middleware unexpectedly applied to /v21")
	}
	_ = res1.Body.Close()

	res2, err := http.Get(ts.URL + "/v2/a")
	if err != nil {
		t.Fatal(err)
	}
	if res2.Header.Get("X-P") != "1" {
		t.Fatalf("prefix middleware not applied to /v2")
	}
	_ = res2.Body.Close()
}

func TestUsePrefixInsideGroupUsesGroupPrefix(t *testing.T) {
	r := New()
	r.Group("/api", func(sub *Router) {
		sub.UsePrefix("/v2", func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				w.Header().Add("X-GP", "1")
				next.ServeHTTP(w, req)
			})
		})
		sub.Get("/v2/ok", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		sub.Get("/v1/no", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
	})

	ts := httptest.NewServer(r)
	defer ts.Close()

	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/api/v2/ok", want: "1"},
		{path: "/api/v1/no", want: ""},
	} {
		res, err := http.Get(ts.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := res.Header.Get("X-GP"); got != tc.want {
			t.Fatalf("path %s: got X-GP=%q want %q", tc.path, got, tc.want)
		}
		_ = res.Body.Close()
	}
}

func TestUsePrefixWithParamRouteDoesNotMatchLiteralLongerPrefix(t *testing.T) {
	r := New()
	r.UsePrefix("/v2/users", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Add("X-P", "1")
			next.ServeHTTP(w, req)
		})
	})
	r.Get("/v2/{resource}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(fmt.Sprintf("%s", Param(req, "resource"))))
	}))

	ts := httptest.NewServer(r)
	defer ts.Close()

	res, err := http.Get(ts.URL + "/v2/users")
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("X-P") != "" {
		t.Fatalf("prefix middleware should not match route param segment for literal prefix")
	}
	_ = res.Body.Close()
}

func TestUseAfterRegistrationDoesNotAffectExistingRoute(t *testing.T) {
	r := New()
	r.Get("/x", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Add("X-LATE", "1")
			next.ServeHTTP(w, req)
		})
	})

	ts := httptest.NewServer(r)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/x")
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Header.Get("X-LATE"); got != "" {
		t.Fatalf("late Use middleware should not affect existing routes, got %q", got)
	}
	_ = res.Body.Close()
}

func TestUsePrefixMatchesWildcardRouteForDeeperPrefix(t *testing.T) {
	r := New()
	r.Get("/v2/{*rest}", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r.UsePrefix("/v2/admin", func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			w.Header().Add("X-W", "1")
			next.ServeHTTP(w, req)
		})
	})

	ts := httptest.NewServer(r)
	defer ts.Close()
	res, err := http.Get(ts.URL + "/v2/anything/here")
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Header.Get("X-W"); got != "1" {
		t.Fatalf("wildcard route should match deeper prefix middleware, got %q", got)
	}
	_ = res.Body.Close()
}

func TestSlashRedirect(t *testing.T) {
	r := New()
	r.Get("/foo/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("trailing"))
	}))
	ts := httptest.NewServer(r)
	defer ts.Close()
	// request without slash should redirect
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/foo", nil)
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusMovedPermanently {
		t.Fatalf("expected redirect got %d", res.StatusCode)
	}
	loc := res.Header.Get("Location")
	if !strings.HasSuffix(loc, "/foo/") {
		t.Fatalf("unexpected location %s", loc)
	}
}
