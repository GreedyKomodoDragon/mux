package mux

import (
	"fmt"
	"net/http"
	"strings"
)

type segmentKind int

const (
	segStatic segmentKind = iota
	segParam
	segWildcard
)

type segment struct {
	kind segmentKind
	name string // for param or wildcard
	raw  string // for static
}

type route struct {
	method      string
	pattern     string
	segments    []segment
	baseHandler http.Handler                      // original handler provided at registration
	middlewares []func(http.Handler) http.Handler // snapshot captured at route registration
	handler     http.Handler
	specificity int  // used for ordering: higher = more specific
	isPrefix    bool // pattern ended with '/'
}

// normalizePath collapses multiple consecutive slashes into one.
func normalizePath(p string) string {
	if p == "" {
		return "/"
	}
	var b []byte
	prevSlash := false
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c == '/' {
			if !prevSlash {
				b = append(b, '/')
				prevSlash = true
			}
		} else {
			b = append(b, c)
			prevSlash = false
		}
	}
	return string(b)
}

// compilePattern parses the pattern into segments and computes specificity.
func compilePattern(p string) ([]segment, int, bool, error) {
	// normalize: remove leading/trailing slash for splitting, but root "/" is special
	p = normalizePath(p)
	if p == "" {
		return nil, 0, false, fmt.Errorf("empty pattern")
	}
	if p == "/" {
		return []segment{}, 100, false, nil
	}
	isPrefix := strings.HasSuffix(p, "/")
	p = strings.TrimPrefix(p, "/")
	if isPrefix {
		p = strings.TrimSuffix(p, "/")
	}
	parts := strings.Split(p, "/")
	segs := make([]segment, 0, len(parts))
	spec := 0
	// track param names to detect duplicates
	seenParams := map[string]struct{}{}
	for i, part := range parts {
		if part == "" {
			return nil, 0, false, fmt.Errorf("empty segment in pattern %q", p)
		}
		// wildcard { *name } or {*name}
		if strings.HasPrefix(part, "{*") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{*"), "}")
			if name == "" {
				return nil, 0, false, fmt.Errorf("empty wildcard name in pattern %q", p)
			}
			if i != len(parts)-1 {
				return nil, 0, false, fmt.Errorf("wildcard must be last segment in pattern %q", p)
			}
			if _, ok := seenParams[name]; ok {
				return nil, 0, false, fmt.Errorf("duplicate param name %q in pattern %q", name, p)
			}
			seenParams[name] = struct{}{}
			segs = append(segs, segment{kind: segWildcard, name: name})
			// wildcard is least specific
			spec += 1
			continue
		}
		// param {name}
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			if name == "" {
				return nil, 0, false, fmt.Errorf("empty param name in pattern %q", p)
			}
			if _, ok := seenParams[name]; ok {
				return nil, 0, false, fmt.Errorf("duplicate param name %q in pattern %q", name, p)
			}
			seenParams[name] = struct{}{}
			segs = append(segs, segment{kind: segParam, name: name})
			// param is medium specificity
			spec += 10
			continue
		}
		// static
		segs = append(segs, segment{kind: segStatic, raw: part})
		// static is most specific
		spec += 100
	}
	return segs, spec, isPrefix, nil
}

// helper predicates used by compilePattern. These are small, testable
// functions that encapsulate the part-format checks and name extraction.
func isWildcardPart(part string) bool {
	return strings.HasPrefix(part, "{*") && strings.HasSuffix(part, "}")
}

func wildcardName(part string) string {
	return strings.TrimSuffix(strings.TrimPrefix(part, "{*"), "}")
}

func isParamPart(part string) bool {
	// param parts are {name} but not wildcard ({*name})
	return strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") && !strings.HasPrefix(part, "{*")
}

func paramName(part string) string {
	return strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
}

// splitPath returns path parts and whether the original path had a trailing slash.
func splitPath(path string) ([]string, bool) {
	path = normalizePath(path)
	if path == "/" {
		return []string{}, strings.HasSuffix(path, "/")
	}
	trailing := strings.HasSuffix(path, "/")
	path = strings.TrimPrefix(path, "/")
	path = strings.TrimSuffix(path, "/")
	parts := strings.Split(path, "/")
	return parts, trailing
}

// segmentsEqual checks whether segs match exactly the parts (for exact matches).
// Returns params and true on full match.
func segmentsEqual(segs []segment, parts []string) (map[string]string, bool) {
	var params map[string]string
	i := 0
	j := 0
	for i < len(segs) && j < len(parts) {
		s := segs[i]
		switch s.kind {
		case segStatic:
			if s.raw != parts[j] {
				return nil, false
			}
			i++
			j++
		case segParam:
			if params == nil {
				params = map[string]string{}
			}
			params[s.name] = parts[j]
			i++
			j++
		case segWildcard:
			if params == nil {
				params = map[string]string{}
			}
			params[s.name] = strings.Join(parts[j:], "/")
			i = len(segs)
			j = len(parts)
		}
	}
	if i == len(segs) && j == len(parts) {
		return params, true
	}
	return nil, false
}

// segmentsPrefixMatch checks whether segs are a prefix of parts (for prefix routes).
// For prefix match we allow parts to be longer than segs; wildcard may capture remainder.
func segmentsPrefixMatch(segs []segment, parts []string) (map[string]string, bool) {
	var params map[string]string
	i := 0
	j := 0

	for i < len(segs) && j < len(parts) {
		s := segs[i]
		switch s.kind {
		case segStatic:
			if s.raw != parts[j] {
				return nil, false
			}
			i++
			j++
		case segParam:
			if params == nil {
				params = map[string]string{}
			}
			params[s.name] = parts[j]
			i++
			j++
		case segWildcard:
			if params == nil {
				params = map[string]string{}
			}
			params[s.name] = strings.Join(parts[j:], "/")
			return params, true
		}
	}
	// if we've consumed all segs, it's a prefix (even if parts longer). If parts shorter, not match.
	if i == len(segs) {
		return params, true
	}
	return nil, false
}
