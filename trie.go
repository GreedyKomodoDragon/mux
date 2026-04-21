package mux

import "net/http"

type trieNode struct {
	staticChildren map[string]*trieNode
	paramChild     *trieNode
	wildcardChild  *trieNode
	routes         map[string]*route
	prefixRoutes   map[string]*route
}

func newTrieNode() *trieNode {
	return &trieNode{
		staticChildren: map[string]*trieNode{},
		routes:         map[string]*route{},
		prefixRoutes:   map[string]*route{},
	}
}

func insertIntoTrie(root *trieNode, rt *route) {
	n := root
	for _, s := range rt.segments {
		switch s.kind {
		case segStatic:
			child, ok := n.staticChildren[s.raw]
			if !ok {
				child = newTrieNode()
				n.staticChildren[s.raw] = child
			}
			n = child
		case segParam:
			if n.paramChild == nil {
				n.paramChild = newTrieNode()
			}
			n = n.paramChild
		case segWildcard:
			if n.wildcardChild == nil {
				n.wildcardChild = newTrieNode()
			}
			n = n.wildcardChild
		}
	}

	if rt.isPrefix {
		n.prefixRoutes[rt.method] = rt
		return
	}
	n.routes[rt.method] = rt
}

func lookupTrie(root *trieNode, parts []string, trailing bool, method string) (*route, map[string]string, []string, bool) {
	n := root
	i := 0
	var allowed []string
	sawPrefixCandidate := false

	for {
		if len(n.prefixRoutes) > 0 {
			if i == len(parts) && !trailing {
				sawPrefixCandidate = true
			} else {
				if rt, ok := n.prefixRoutes[method]; ok {
					return rt, paramsForRoute(rt, parts), nil, sawPrefixCandidate
				}
				allowed = appendMethods(allowed, n.prefixRoutes)
			}
		}

		if i == len(parts) {
			break
		}

		part := parts[i]
		if child, ok := n.staticChildren[part]; ok {
			n = child
			i++
			continue
		}
		if n.paramChild != nil {
			n = n.paramChild
			i++
			continue
		}
		if n.wildcardChild != nil {
			n = n.wildcardChild
			i = len(parts)
			continue
		}

		return nil, nil, allowed, sawPrefixCandidate
	}

	if rt, ok := n.routes[method]; ok {
		return rt, paramsForRoute(rt, parts), nil, sawPrefixCandidate
	}
	allowed = appendMethods(allowed, n.routes)

	if n.wildcardChild != nil {
		wn := n.wildcardChild
		if len(wn.prefixRoutes) > 0 {
			if i == len(parts) && !trailing {
				sawPrefixCandidate = true
			} else {
				if rt, ok := wn.prefixRoutes[method]; ok {
					return rt, paramsForRoute(rt, parts), nil, sawPrefixCandidate
				}
				allowed = appendMethods(allowed, wn.prefixRoutes)
			}
		}
		if rt, ok := wn.routes[method]; ok {
			return rt, paramsForRoute(rt, parts), nil, sawPrefixCandidate
		}
		allowed = appendMethods(allowed, wn.routes)
	}

	return nil, nil, allowed, sawPrefixCandidate
}

func paramsForRoute(rt *route, parts []string) map[string]string {
	if rt.isPrefix {
		if p, ok := segmentsPrefixMatch(rt.segments, parts); ok {
			return p
		}
		return nil
	}
	p, ok := segmentsEqual(rt.segments, parts)
	if !ok {
		return nil
	}
	return p
}

func appendMethods(dst []string, byMethod map[string]*route) []string {
	for m := range byMethod {
		dst = appendIfMissing(dst, m)
	}
	return dst
}

func hasExactPath(root *trieNode, parts []string) bool {
	n := root
	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if child, ok := n.staticChildren[part]; ok {
			n = child
			continue
		}
		if n.paramChild != nil {
			n = n.paramChild
			continue
		}
		if n.wildcardChild != nil {
			return true
		}
		return false
	}

	if len(n.routes) > 0 {
		return true
	}
	if n.wildcardChild != nil && len(n.wildcardChild.routes) > 0 {
		return true
	}
	return false
}

func walkRoutes(root *trieNode, fn func(*route)) {
	stack := []*trieNode{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		for _, rt := range n.routes {
			fn(rt)
		}
		for _, rt := range n.prefixRoutes {
			fn(rt)
		}

		if n.paramChild != nil {
			stack = append(stack, n.paramChild)
		}
		if n.wildcardChild != nil {
			stack = append(stack, n.wildcardChild)
		}
		for _, c := range n.staticChildren {
			stack = append(stack, c)
		}
	}
}

var _ = http.MethodGet
