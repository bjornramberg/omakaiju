package version

import "testing"

// The version is what a bug report quotes, so the form it takes matters.
func TestDisplay(t *testing.T) {
	// Display reads the package variable, which the build stamps, so each
	// case sets it and puts it back afterwards.
	original := Version
	t.Cleanup(func() { Version = original })

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"the bare version", "0.9.1", "v0.9.1"},
		{"a version that already carries the v", "v0.9.1", "v0.9.1"},
		{"a build ahead of a tag", "0.10.0-4-g2b83849", "v0.10.0-4-g2b83849"},
		{"a dirty build", "0.10.0-4-g2b83849-dirty", "v0.10.0-4-g2b83849-dirty"},
		{"an unstamped build", "dev", "dev"},
		{"nothing at all", "", "dev"},
		{"a version with spaces around it", "  0.9.1  ", "v0.9.1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			Version = tc.in
			if got := Display(); got != tc.want {
				t.Errorf("Display() with %q = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
