package controlplane

import "testing"

func TestNormalizeEmailForAliasDedup(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase and trim", "  Ab@Gmail.com ", "ab@gmail.com"},
		{"gmail plus suffix", "ab+tag@gmail.com", "ab@gmail.com"},
		{"gmail dots", "a.b@gmail.com", "ab@gmail.com"},
		{"googlemail folds to gmail", "ab@googlemail.com", "ab@gmail.com"},
		{"gmail dots and plus", "a.b+promo@googlemail.com", "ab@gmail.com"},
		{"fqdn root dot", "ab@gmail.com.", "ab@gmail.com"},
		{"non gmail keeps dots", "a.b@example.com", "a.b@example.com"},
		{"non gmail strips plus", "a.b+tag@example.com", "a.b@example.com"},
		{"leading plus keeps local", "+tag@example.com", "+tag@example.com"},
		{"malformed returns normalized input", "not-an-email", "not-an-email"},
		{"empty local part", "@example.com", "@example.com"},
		{"dot only local keeps original", "...@gmail.com", "...@gmail.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeEmailForAliasDedup(tc.input); got != tc.want {
				t.Fatalf("NormalizeEmailForAliasDedup(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestNormalizeEmailForAliasDedupCollisions(t *testing.T) {
	base := NormalizeEmailForAliasDedup("ab@gmail.com")
	aliases := []string{
		"AB@gmail.com",
		"ab+1@gmail.com",
		"a.b@gmail.com",
		"ab@googlemail.com",
		"a.b+2@googlemail.com",
		"ab@gmail.com.",
	}
	for _, alias := range aliases {
		if got := NormalizeEmailForAliasDedup(alias); got != base {
			t.Fatalf("alias %q normalized to %q, want collision with %q", alias, got, base)
		}
	}
	distinct := []string{"a.b@example.com", "ab@example.com", "abc@gmail.com"}
	for _, other := range distinct {
		if NormalizeEmailForAliasDedup(other) == base {
			t.Fatalf("address %q must not collide with %q", other, base)
		}
	}
}
