package llm

import (
	"testing"
)

func TestIsRemoteSocket(t *testing.T) {
	tests := []struct {
		socketPath string
		want       bool
	}{
		{"/var/run/docker.sock", false},
		{"tcp://localhost:11434", true},
		{"tcp://192.168.1.100:11434", true},
		{"localhost:11434", true},
		{"192.168.1.100:8080", true},
		{"/home/user/.docker/model-runner.sock", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.socketPath, func(t *testing.T) {
			got := IsRemoteSocket(tt.socketPath)
			if got != tt.want {
				t.Errorf("IsRemoteSocket(%q) = %v, want %v", tt.socketPath, got, tt.want)
			}
		})
	}
}

func TestParseSocketAddr(t *testing.T) {
	tests := []struct {
		socketPath  string
		wantNetwork string
		wantAddr    string
	}{
		{
			socketPath:  "tcp://localhost:11434",
			wantNetwork: "tcp",
			wantAddr:    "localhost:11434",
		},
		{
			socketPath:  "tcp://192.168.1.100:8080",
			wantNetwork: "tcp",
			wantAddr:    "192.168.1.100:8080",
		},
		{
			socketPath:  "localhost:11434",
			wantNetwork: "tcp",
			wantAddr:    "localhost:11434",
		},
		{
			socketPath:  "/var/run/docker.sock",
			wantNetwork: "unix",
			wantAddr:    "/var/run/docker.sock",
		},
		{
			socketPath:  "/home/user/.docker/model-runner.sock",
			wantNetwork: "unix",
			wantAddr:    "/home/user/.docker/model-runner.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.socketPath, func(t *testing.T) {
			gotNet, gotAddr := parseSocketAddr(tt.socketPath)
			if gotNet != tt.wantNetwork {
				t.Errorf("parseSocketAddr(%q) network = %q, want %q", tt.socketPath, gotNet, tt.wantNetwork)
			}
			if gotAddr != tt.wantAddr {
				t.Errorf("parseSocketAddr(%q) addr = %q, want %q", tt.socketPath, gotAddr, tt.wantAddr)
			}
		})
	}
}

func TestIsRepetitive(t *testing.T) {
	tests := []struct {
		name    string
		prev    string
		current string
		want    bool
	}{
		{
			name:    "empty prev",
			prev:    "",
			current: "some content",
			want:    false,
		},
		{
			name:    "empty current",
			prev:    "some content",
			current: "",
			want:    false,
		},
		{
			name:    "both empty",
			prev:    "",
			current: "",
			want:    false,
		},
		{
			name:    "short current (less than 50 chars)",
			prev:    "some previous content that is long enough",
			current: "short",
			want:    false,
		},
		{
			name:    "identical long content",
			prev:    longString(300),
			current: longString(300),
			want:    true,
		},
		{
			name:    "current starts with prev content",
			prev:    longString(250),
			current: longString(250),
			want:    true,
		},
		{
			name:    "completely different content",
			prev:    longStringChar('a', 200),
			current: longStringChar('b', 200),
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRepetitive(tt.prev, tt.current)
			if got != tt.want {
				t.Errorf("isRepetitive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMaxContinuations(t *testing.T) {
	if maxContinuations != 3 {
		t.Errorf("maxContinuations = %d, want 3", maxContinuations)
	}
}

// longString creates a string of length n using a repeating pattern.
func longString(n int) string {
	pattern := "The quick brown fox jumps over the lazy dog. "
	result := ""
	for len(result) < n {
		result += pattern
	}
	return result[:n]
}

// longStringChar creates a string of length n using a single character.
func longStringChar(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
