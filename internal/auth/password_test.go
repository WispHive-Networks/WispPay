package auth

import "testing"

func TestPasswordHashVerifiesOnlyOriginalPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("hash did not verify original password")
	}
	if VerifyPassword(hash, "different password") {
		t.Fatal("hash verified a different password")
	}
	if VerifyPassword("invalid", "correct horse battery staple") {
		t.Fatal("malformed password hash verified")
	}
}

func TestHashPasswordRejectsOutOfRangePasswords(t *testing.T) {
	for _, password := range []string{"short", string(make([]byte, 129))} {
		if _, err := HashPassword(password); err == nil {
			t.Fatal("expected invalid password length to fail")
		}
	}
}
