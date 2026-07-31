package token

import "testing"

func TestGenerate_HasPrefixAndHash(t *testing.T) {
	got, err := Generate("landing")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if !stringsHasPrefix(got.Plaintext, "pv_") {
		t.Fatalf("plaintext prefix: %q", got.Plaintext)
	}
	if got.Hash != HashPlaintext(got.Plaintext) {
		t.Fatal("hash mismatch")
	}
	if !labelPattern.MatchString(got.Subdomain) {
		t.Fatalf("subdomain invalid: %q", got.Subdomain)
	}
	if !stringsHasPrefix(got.Subdomain, "landing-") {
		t.Fatalf("expected landing slug, got %q", got.Subdomain)
	}
}

func TestSlugFromProjectName_Transliterates(t *testing.T) {
	slug := slugFromProjectName("Привет Мир")
	if slug == "" || stringsContainsSpace(slug) {
		t.Fatalf("bad slug: %q", slug)
	}
}

func TestSlugFromProjectName_EmptyFallsBack(t *testing.T) {
	slug := slugFromProjectName("   ")
	if !labelPattern.MatchString(slug+"-abcdefghij") && len(slug) < 3 {
		t.Fatalf("fallback slug unexpected: %q", slug)
	}
}

func stringsHasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func stringsContainsSpace(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			return true
		}
	}
	return false
}
