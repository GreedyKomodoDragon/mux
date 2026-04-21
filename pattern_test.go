package mux

import "testing"

func TestIsWildcardPart(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"{*path}", true},
		{"{*}", true},
		{"{name}", false},
		{"plain", false},
	}
	for _, tt := range tests {
		if got := isWildcardPart(tt.in); got != tt.want {
			t.Fatalf("isWildcardPart(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestWildcardName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"{*path}", "path"},
		{"{*}", ""},
	}
	for _, tt := range tests {
		if got := wildcardName(tt.in); got != tt.want {
			t.Fatalf("wildcardName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestIsParamPart(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"{id}", true},
		{"{*id}", false},
		{"no", false},
	}
	for _, tt := range tests {
		if got := isParamPart(tt.in); got != tt.want {
			t.Fatalf("isParamPart(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParamName(t *testing.T) {
	if got := paramName("{id}"); got != "id" {
		t.Fatalf("paramName = %q", got)
	}
	if got := paramName("{}"); got != "" {
		t.Fatalf("paramName empty = %q", got)
	}
}
