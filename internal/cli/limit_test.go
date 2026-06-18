package cli

import "testing"

func TestParseLimitDuration(t *testing.T) {
	tests := map[string]int{
		"30m":   1800,
		"1h":    3600,
		"1h30m": 5400,
	}

	for input, want := range tests {
		got, err := parseLimitDuration(input)
		if err != nil {
			t.Fatalf("parseLimitDuration(%q) error = %v", input, err)
		}
		if got != want {
			t.Fatalf("parseLimitDuration(%q) = %d, want %d", input, got, want)
		}
	}
}

func TestParseLimitDurationRejectsInvalidValues(t *testing.T) {
	for _, input := range []string{"", "abc", "0s", "-1m"} {
		if _, err := parseLimitDuration(input); err == nil {
			t.Fatalf("parseLimitDuration(%q) expected error", input)
		}
	}
}

func TestFormatSeconds(t *testing.T) {
	tests := map[int]string{
		0:    "0s",
		45:   "45s",
		1800: "30m",
		5400: "1h30m",
	}

	for input, want := range tests {
		if got := formatSeconds(input); got != want {
			t.Fatalf("formatSeconds(%d) = %q, want %q", input, got, want)
		}
	}
}
