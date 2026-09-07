package adminauth

import (
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	encoded := hashPassword("correct horse battery staple")
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
	first := hashPassword("same")
	second := hashPassword("same")
	if first == second {
		t.Fatal("hashes of the same password must differ by salt")
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
