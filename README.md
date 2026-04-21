# mux

Lightweight, dependency-free HTTP router

Quick example:

```go
package main

import (
  "net/http"
  "github.com/GreedyKomodoDragon/mux"
)

func main() {
  r := mux.New()
  r.Get("/users/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    id := mux.Param(r, "id")
    w.Write([]byte(id))
  }))
  http.ListenAndServe(":8080", r)
}
```

Features:
- Named params: `{name}`
- Explicit wildcard: `{*path}` (must be last segment)
- Subrouter groups: `Group(prefix, func(sub *Router) { ... })`
- Per-router middleware with `Use(...)`
- Prefix middleware with `UsePrefix(prefix, ...)`
- Convenience methods: `Get/Post/Put/Patch/Delete`
- Trailing slash behaviour mirrors net/http/ServeMux (redirects to add/remove trailing slash when a nearby route exists)

How it works:
- Route registration compiles a pattern into typed path segments (`static`, `param`, `wildcard`) and inserts it into an internal trie.
- Request matching walks the trie by path segment with precedence: `static` -> `param` -> `wildcard`.
- Handlers are pre-wrapped at registration time (not per request), so serving stays fast.
- URL params are stored on request context only when needed, and read with `Param(r, name)` / `Params(r)`.
- Internal trie details are documented in `mux/TRIE.md`.

Pattern rules:
- `{name}` captures one segment.
- `{*name}` captures the remaining path and must be the final segment.
- Duplicate param names inside one route pattern are invalid and panic at registration.
- Empty segments (for example `/a//b`) are invalid and panic at registration.

Middleware model:
- `UsePrefix(prefix, ...)` applies middleware to matching routes by path prefix.
- Prefix matching is segment-aware: `/v2` matches `/v2/users`, not `/v21/users`.
- Literal prefix segments only match static route segments (not param segments).
- Wildcard route segments (`{*name}`) can satisfy deeper prefix matches.
- Middleware execution order is: `UsePrefix(...)` -> `Use(...)` -> handler.
- `UsePrefix(...)` rewraps existing matching routes immediately and also affects future matching routes.
- `Use(...)` is registration-time only; adding it later does not rewrap existing routes.

Groups:
- `Group(prefix, fn)` creates a subrouter with inherited `Use(...)` middleware and combined prefix.
- Routes registered in a group are inserted into the root router trie with the group prefix applied.
- `UsePrefix(...)` called inside a group is interpreted relative to that group prefix.

HTTP behavior:
- If path matches but method does not, router returns `405` and sets `Allow`.
- Not found routes use the configured `NotFound` handler (default `http.NotFound`).
- Trailing slash redirect semantics follow `net/http/ServeMux` behavior.

Notes:
- Register routes at startup; the router is optimized for serving and does not expect frequent runtime registration.
- Duplicate registrations (same method+pattern) are allowed; the last registration wins.
