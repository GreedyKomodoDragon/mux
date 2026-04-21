# Trie Routing Internals

This document explains how `mux` matches routes using a segment trie.

## Overview

`mux` compiles each route pattern into path segments, then inserts those
segments into a trie (prefix tree). Each trie edge represents one path segment.

At request time, the router walks the trie by request path segment, using a
fixed precedence:

1. static segment
2. param segment (`{name}`)
3. wildcard segment (`{*name}`)

This keeps route lookup predictable and fast.

## Segment model

A pattern is compiled into a list of `segment` values:

- `segStatic`: literal path segment, for example `users`
- `segParam`: single-segment capture, for example `{id}`
- `segWildcard`: catch-all capture, for example `{*rest}` (must be last)

Examples:

- `/users/{id}` -> `static("users")`, `param("id")`
- `/assets/{*path}` -> `static("assets")`, `wildcard("path")`

Validation happens at registration:

- empty pattern is invalid
- empty segment is invalid (`/a//b`)
- wildcard must be final segment
- duplicate param names in one pattern are invalid

Invalid patterns panic during registration (fail fast).

## Trie node shape

Each trie node stores possible next steps and terminal handlers:

- `staticChildren map[string]*trieNode`
- `paramChild *trieNode`
- `wildcardChild *trieNode`
- `routes map[method]*route` (exact routes)
- `prefixRoutes map[method]*route` (routes registered with trailing slash)

The router keeps one root trie. Group routes are inserted into the same root
trie with the group prefix applied.

## Registration flow

When you register a route (`Get`, `Post`, `Handle`, etc):

1. Pattern is normalized and compiled into segments.
2. Middleware snapshots are captured (route middleware + prefix middleware).
3. Final handler is pre-composed (not per request).
4. Route is inserted into trie along its segment path.
5. Final node stores handler by HTTP method.

Duplicate method+pattern registrations overwrite previous ones.

## Request matching flow

For request path `/a/b/c`:

1. Split request path into segments (`["a", "b", "c"]`).
2. Start at root node and walk segment-by-segment.
3. At each segment, try in order: static child, param child, wildcard child.
4. If a terminal node has a handler for request method, dispatch it.
5. If path matched but method did not, return `405` with `Allow` header.
6. If no match, apply trailing-slash redirect checks, then `NotFound`.

Params are extracted from the matched route and injected into request context.
`Param(r, "name")` and `Params(r)` read those values.

## Why this is faster than linear scans

The old approach checked routes one-by-one. Trie lookup instead follows only
one path through shared prefixes. For typical static-heavy APIs, this reduces
comparison work significantly as route count grows.

## Middleware interaction

`mux` composes handlers at registration time:

- order: `UsePrefix(...)` -> `Use(...)` -> handler
- `UsePrefix` rewraps existing matching routes and applies to future matches
- `Use` applies only to routes registered after the call

Trie lookup itself only finds the route and calls its already-composed handler.

## Prefix middleware and segment-aware matching

Prefix middleware uses segment-aware checks:

- `/v2` matches `/v2/users`
- `/v2` does not match `/v21/users`
- literal prefix segments do not match route param segments
- wildcard route segments can satisfy deeper prefixes

This prevents accidental broad matches.

## Trailing slash semantics

The router mirrors `net/http/ServeMux`-style trailing-slash redirects:

- if a nearby slash-variant route exists, it can redirect to add/remove slash
- method handling and 405 behavior still apply for matched paths

## Concurrency and safety

- Registration and middleware updates are protected by router mutexes.
- Request serving uses read locking for trie lookup.
- Route handlers are composed ahead of time to keep request path hot.
