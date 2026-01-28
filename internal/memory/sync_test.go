package memory

import "testing"

func TestContains(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		keyword string
		want    bool
	}{
		// Substring matching (the bug fix)
		{
			name:    "substring in middle",
			text:    "context deadline exceeded",
			keyword: "deadline",
			want:    true,
		},
		{
			name:    "substring not found",
			text:    "context deadline exceeded",
			keyword: "timeout",
			want:    false,
		},

		// Prefix and suffix (should still work)
		{
			name:    "prefix match",
			text:    "deadline exceeded",
			keyword: "deadline",
			want:    true,
		},
		{
			name:    "suffix match",
			text:    "context deadline",
			keyword: "deadline",
			want:    true,
		},

		// Exact match
		{
			name:    "exact match",
			text:    "deadline",
			keyword: "deadline",
			want:    true,
		},

		// Case insensitive matching
		{
			name:    "case insensitive - keyword uppercase",
			text:    "context deadline exceeded",
			keyword: "DEADLINE",
			want:    true,
		},
		{
			name:    "case insensitive - text uppercase",
			text:    "CONTEXT DEADLINE EXCEEDED",
			keyword: "deadline",
			want:    true,
		},
		{
			name:    "case insensitive - mixed case",
			text:    "Context DeadLine Exceeded",
			keyword: "dEaDlInE",
			want:    true,
		},

		// Edge cases
		{
			name:    "empty text",
			text:    "",
			keyword: "deadline",
			want:    false,
		},
		{
			name:    "empty keyword",
			text:    "context deadline exceeded",
			keyword: "",
			want:    true,
		},
		{
			name:    "both empty",
			text:    "",
			keyword: "",
			want:    true,
		},
		{
			name:    "keyword longer than text",
			text:    "ctx",
			keyword: "context",
			want:    false,
		},
		{
			name:    "single character match",
			text:    "error",
			keyword: "r",
			want:    true,
		},
		{
			name:    "single character no match",
			text:    "error",
			keyword: "x",
			want:    false,
		},

		// Real-world patterns
		{
			name:    "error pattern",
			text:    "connection reset by peer",
			keyword: "reset",
			want:    true,
		},
		{
			name:    "api error pattern",
			text:    "API rate limit exceeded",
			keyword: "rate limit",
			want:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := contains(tt.text, tt.keyword)
			if got != tt.want {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.text, tt.keyword, got, tt.want)
			}
		})
	}
}
