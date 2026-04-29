package mux

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// Router is a lightweight HTTP router with named params, explicit wildcard,
// subrouter groups, and per-router middleware. It's intentionally small and
// dependency-free so it can be published later as a standalone module.
type Router struct {
	prefix           string
	middlewares      []func(http.Handler) http.Handler
	tree             *trieNode
	notFound         http.Handler
	methodNotAllowed http.Handler
	prefixMws        []prefixMW
	mu               sync.RWMutex // protects trie and middleware registration
	root             *Router
}

type prefixMW struct {
	parts []string
	raw   string
	mw    func(http.Handler) http.Handler
}

// New returns a new Router.
func New() *Router {
	r := &Router{
		notFound:         http.NotFoundHandler(),
		methodNotAllowed: http.HandlerFunc(defaultMethodNotAllowed),
		tree:             newTrieNode(),
	}
	r.root = r
	return r
}

func defaultMethodNotAllowed(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

// Handle registers a handler for the given method and pattern.
// Pattern syntax: segments separated by '/'. Named param: {name}. Wildcard: {*name} (must be last).
func (r *Router) Handle(method, pattern string, h http.Handler) {
	// Lock ordering rule: never hold a child router lock while acquiring the
	// root router lock. We first snapshot child state, then lock root only.
	r.mu.RLock()
	full := joinPrefix(r.prefix, pattern)
	root := r.root
	routeMws := append([]func(http.Handler) http.Handler(nil), r.middlewares...)
	r.mu.RUnlock()
	if root == nil {
		root = r
	}

	seg, spec, isPrefix, err := compilePattern(full)
	if err != nil {
		panic(err)
	}
	// Snapshot middleware slices at registration time.
	base := h

	root.mu.Lock()
	rootPmws := append([]prefixMW(nil), root.prefixMws...)
	wrapped := composeHandler(base, seg, routeMws, rootPmws)
	rt := &route{
		method:      method,
		pattern:     full,
		segments:    seg,
		baseHandler: base,
		middlewares: routeMws,
		handler:     wrapped,
		specificity: spec,
		isPrefix:    isPrefix,
	}
	insertIntoTrie(root.tree, rt)
	root.mu.Unlock()
}

// HandleFunc convenience wrapper.
func (r *Router) HandleFunc(method, pattern string, hf func(http.ResponseWriter, *http.Request)) {
	r.Handle(method, pattern, http.HandlerFunc(hf))
}

// HandleMethods registers the same handler for multiple HTTP methods.
func (r *Router) HandleMethods(methods []string, pattern string, h http.Handler) {
	for _, m := range methods {
		r.Handle(m, pattern, h)
	}
}

// HandleFuncMethods convenience wrapper for HandleMethods.
func (r *Router) HandleFuncMethods(methods []string, pattern string, hf func(http.ResponseWriter, *http.Request)) {
	r.HandleMethods(methods, pattern, http.HandlerFunc(hf))
}

// Convenience methods.
func (r *Router) Get(pattern string, h http.Handler)    { r.Handle(http.MethodGet, pattern, h) }
func (r *Router) Post(pattern string, h http.Handler)   { r.Handle(http.MethodPost, pattern, h) }
func (r *Router) Put(pattern string, h http.Handler)    { r.Handle(http.MethodPut, pattern, h) }
func (r *Router) Patch(pattern string, h http.Handler)  { r.Handle(http.MethodPatch, pattern, h) }
func (r *Router) Delete(pattern string, h http.Handler) { r.Handle(http.MethodDelete, pattern, h) }

// Use appends middleware to this router. Middleware will be applied in the
// order they are provided when wrapping handlers at registration time.
func (r *Router) Use(mw ...func(http.Handler) http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.middlewares = append(r.middlewares, mw...)
}

// UsePrefix registers middleware that applies to all routes whose registered
// pattern matches the provided prefix by path segments. The prefix is
// normalized and matched in a segment-aware manner. UsePrefix applies the
// middleware to existing routes immediately and to future registrations.
func (r *Router) UsePrefix(prefix string, mw ...func(http.Handler) http.Handler) {
	// normalize prefix relative to this router's prefix
	full := joinPrefix(r.prefix, prefix)
	parts, _ := splitPath(full)
	root := r.root
	if root == nil {
		root = r
	}

	root.mu.Lock()
	defer root.mu.Unlock()

	for _, m := range mw {
		root.prefixMws = append(root.prefixMws, prefixMW{parts: parts, raw: full, mw: m})
	}

	// Rewrap existing routes that match the new prefix.
	rootPmws := append([]prefixMW(nil), root.prefixMws...)
	walkRoutes(root.tree, func(rt *route) {
		if prefixMatchesRoute(parts, rt) {
			rt.handler = composeHandler(rt.baseHandler, rt.segments, rt.middlewares, rootPmws)
		}
	})
}

// Group creates a subrouter with the provided prefix. The subrouter inherits
// middleware from the parent. Registration inside fn will register routes on
// the parent router but with the group's prefix applied.
func (r *Router) Group(prefix string, fn func(sub *Router)) {
	r.mu.Lock()
	// child shares the parent's middleware slice (copied) and will register into parent
	child := &Router{
		prefix:           joinPrefix(r.prefix, prefix),
		middlewares:      append([]func(http.Handler) http.Handler(nil), r.middlewares...),
		tree:             r.root.tree,
		notFound:         r.notFound,
		methodNotAllowed: r.methodNotAllowed,
		root:             r.root,
	}
	r.mu.Unlock()
	fn(child)
	// child.Handle will add routes into parent.routes via root pointer
}

// NotFound sets a custom NotFound handler.
func (r *Router) NotFound(h http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notFound = h
}

// MethodNotAllowed sets a custom 405 handler.
func (r *Router) MethodNotAllowed(h http.Handler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methodNotAllowed = h
}

// ServeHTTP implements http.Handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	// Normalize: trim duplicate slashes but keep leading/trailing as-is for now.
	if path == "" {
		path = "/"
	}

	r.mu.RLock()
	tree := r.tree
	notFound := r.notFound
	methodNotAllowed := r.methodNotAllowed
	parts, trailing := splitPath(path)
	rt, params, allowed, sawPrefixCandidate := lookupTrie(tree, parts, trailing, req.Method)
	r.mu.RUnlock()
	if rt != nil {
		if len(params) == 0 {
			rt.handler.ServeHTTP(w, req)
			return
		}
		ctx := context.WithValue(req.Context(), paramsKey{}, params)
		rt.handler.ServeHTTP(w, req.WithContext(ctx))
		return
	}

	if len(allowed) > 0 {
		// 405
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		methodNotAllowed.ServeHTTP(w, req)
		return
	}

	// No path matched the request. Consider trailing-slash redirect behaviour
	// only when there are no allowed methods for the original path. This
	// mirrors net/http ServeMux which redirects only when the exact path has
	// no handler but an alternate with/without slash exists.
	alt := altPathForSlash(path)
	if alt != path {
		// If the earlier loop found a prefix candidate, issue redirect
		if sawPrefixCandidate {
			http.Redirect(w, req, alt, http.StatusMovedPermanently)
			return
		}
		// Fallback: check alternate path matches any route.
		altParts, _ := splitPath(alt)
		if hasExactPath(tree, altParts) {
			http.Redirect(w, req, alt, http.StatusMovedPermanently)
			return
		}
	}

	notFound.ServeHTTP(w, req)
}

func appendIfMissing(a []string, m string) []string {
	if slices.Contains(a, m) {
		return a
	}

	return append(a, m)
}

// composeHandler builds the final handler by applying prefix middlewares and
// router middlewares in the correct order.
// Order: prefix middlewares (outermost) -> router.Use middlewares -> base handler.
func composeHandler(base http.Handler, routeSegments []segment, routeMws []func(http.Handler) http.Handler, rootPmws []prefixMW) http.Handler {
	// Collect matching prefix middlewares in registration order.
	var pmws []func(http.Handler) http.Handler
	for _, pm := range rootPmws {
		if prefixMatchesSegments(pm.parts, routeSegments) {
			pmws = append(pmws, pm.mw)
		}
	}

	wrapped := base
	// First apply router middlewares in reverse so first Use() is outermost in that group.
	for i := len(routeMws) - 1; i >= 0; i-- {
		wrapped = routeMws[i](wrapped)
	}
	// Then apply prefix middlewares in reverse so first UsePrefix() is outermost overall.
	for i := len(pmws) - 1; i >= 0; i-- {
		wrapped = pmws[i](wrapped)
	}

	return wrapped
}

// prefixMatchesRoute checks whether the prefix (as parts) matches the route's
// segments in a segment-aware way.
func prefixMatchesRoute(prefixParts []string, rt *route) bool {
	return prefixMatchesSegments(prefixParts, rt.segments)
}

func prefixMatchesSegments(prefixParts []string, segs []segment) bool {
	// root prefix (empty parts) matches everything
	if len(prefixParts) == 0 {
		return true
	}
	j := 0
	for i := 0; i < len(prefixParts); i++ {
		if j >= len(segs) {
			// route shorter than prefix: only match if last route seg is wildcard
			if len(segs) > 0 && segs[len(segs)-1].kind == segWildcard {
				return true
			}
			return false
		}
		s := segs[j]
		switch s.kind {
		case segStatic:
			if s.raw != prefixParts[i] {
				return false
			}
			j++
		case segParam:
			// param segment in route cannot match a literal prefix
			return false
		case segWildcard:
			// wildcard in route can consume remainder and thus match
			return true
		}
	}
	return true
}

func joinPrefix(prefix, pattern string) string {
	if prefix == "" {
		return pattern
	}
	if pattern == "" || pattern == "/" {
		return prefix
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(pattern, "/")
}

// altPathForSlash returns the alternative path that toggles a trailing slash
// (mimics net/http ServeMux behaviour for slash redirects).
func altPathForSlash(p string) string {
	if p == "/" {
		return p
	}

	if before, ok := strings.CutSuffix(p, "/"); ok {
		return before
	}

	return p + "/"
}
