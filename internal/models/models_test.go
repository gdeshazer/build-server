package models

import "testing"

func TestCommitStatus(t *testing.T) {
	tests := []struct {
		name          string
		local, remote string
		want          string
	}{
		{"both empty", "", "", "unknown"},
		{"local empty", "", "abc123", "unknown"},
		{"remote empty", "abc123", "", "unknown"},
		{"equal", "abc123", "abc123", "up-to-date"},
		{"different", "abc123", "def456", "diverged"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Repository{LocalCommit: tc.local, RemoteCommit: tc.remote}
			if got := r.CommitStatus(); got != tc.want {
				t.Errorf("CommitStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShortHash(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"5 chars", "abcde", "abcde"},
		{"7 chars", "abcdefg", "abcdefg"},
		{"40 chars", "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2", "a1b2c3d"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := &Repository{LocalCommit: tc.input}
			if got := r.ShortLocal(); got != tc.want {
				t.Errorf("ShortLocal(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{"success", true},
		{"failed", true},
		{"cancelled", true},
		{"pending", false},
		{"running", false},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			b := &BuildRun{Status: tc.status}
			if got := b.IsTerminal(); got != tc.want {
				t.Errorf("IsTerminal(%q) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}
