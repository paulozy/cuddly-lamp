package postgres

import "testing"

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "plain", in: "auth handler", want: "auth handler"},
		{name: "percent", in: "100% coverage", want: `100\% coverage`},
		{name: "underscore", in: "user_id", want: `user\_id`},
		{name: "backslash", in: `path\to\file`, want: `path\\to\\file`},
		{name: "combined", in: `repo_%\path`, want: `repo\_\%\\path`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := escapeLike(tt.in); got != tt.want {
				t.Fatalf("escapeLike(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
