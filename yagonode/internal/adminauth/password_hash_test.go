package adminauth

import (
	"errors"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	encoded, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	if encoded == "" {
		t.Fatal("encoded hash is empty")
	}

	ok, err := verifyPassword(encoded, "correct horse battery staple")
	if err != nil {
		t.Fatalf("verifyPassword: %v", err)
	}
	if !ok {
		t.Fatal("correct password did not verify")
	}

	ok, err = verifyPassword(encoded, "wrong password")
	if err != nil {
		t.Fatalf("verifyPassword wrong: %v", err)
	}
	if ok {
		t.Fatal("wrong password verified")
	}
}

func TestHashPasswordDistinctSalts(t *testing.T) {
	first, err := hashPassword("same")
	if err != nil {
		t.Fatalf("hashPassword first: %v", err)
	}
	second, err := hashPassword("same")
	if err != nil {
		t.Fatalf("hashPassword second: %v", err)
	}
	if first == second {
		t.Fatal("hashes of the same password must differ by salt")
	}
}

func TestHashPasswordReportsRandomFailure(t *testing.T) {
	original := randRead
	randRead = func([]byte) (int, error) { return 0, errors.New("no entropy") }
	t.Cleanup(func() { randRead = original })

	if _, err := hashPassword("x"); err == nil {
		t.Fatal("hashPassword should fail when the random source fails")
	}
}

func TestVerifyRejectsMalformedHash(t *testing.T) {
	cases := map[string]string{
		"not a hash":          "definitely-not-a-phc-string",
		"wrong field count":   "$argon2id$v=19",
		"leading segment":     "a$b$c$d$e$f",
		"wrong algorithm":     "$argon2i$v=19$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"bad version scan":    "$argon2id$vX$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"unsupported version": "$argon2id$v=18$m=65536,t=3,p=2$c2FsdA$aGFzaA",
		"bad params":          "$argon2id$v=19$broken$c2FsdA$aGFzaA",
		"bad salt base64":     "$argon2id$v=19$m=65536,t=3,p=2$@@@$aGFzaA",
		"bad key base64":      "$argon2id$v=19$m=65536,t=3,p=2$c2FsdA$@@@",
	}
	for name, encoded := range cases {
		t.Run(name, func(t *testing.T) {
			ok, err := verifyPassword(encoded, "whatever")
			if err == nil {
				t.Fatalf("expected error for %q", encoded)
			}
			if ok {
				t.Fatal("malformed hash must not verify")
			}
		})
	}
}
