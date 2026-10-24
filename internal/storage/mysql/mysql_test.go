package mysql

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestConnectionErrorCategoryDoesNotExposeRawError(t *testing.T) {
	category := ConnectionErrorCategory(errors.New("dial failed for user:top-secret-password"))
	if strings.Contains(category, "top-secret-password") || strings.Contains(category, "user:") {
		t.Fatalf("raw connection details escaped: %q", category)
	}
}

func TestSafePingContextConvertsHandshakePanicToError(t *testing.T) {
	err := safePingContext(context.Background(), func(context.Context) error {
		panic("malformed handshake")
	})
	if err == nil || err.Error() != "database server returned an invalid handshake" {
		t.Fatalf("safe ping error = %v", err)
	}
}

func TestConnectionErrorCategoryRecognizesCommonFailures(t *testing.T) {
	for _, test := range []struct {
		message string
		want    string
	}{
		{message: "dial tcp: lookup db on resolver: no such host", want: "database host could not be resolved"},
		{message: "dial tcp: connect: connection refused", want: "database host refused the connection"},
		{message: "dial tcp: i/o timeout", want: "database connection timed out"},
		{message: "database server returned an invalid handshake", want: "database endpoint did not return a valid MySQL handshake"},
		{message: "tls: failed to verify certificate: x509: certificate signed by unknown authority", want: "database TLS certificate authority is not trusted"},
	} {
		t.Run(test.want, func(t *testing.T) {
			if got := ConnectionErrorCategory(errors.New(test.message)); got != test.want {
				t.Fatalf("category = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPublicInvoiceTokenEncodingAndHashing(t *testing.T) {
	first, err := randomTokenBytes()
	if err != nil {
		t.Fatal(err)
	}
	second, err := randomTokenBytes()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(first)
	if err != nil || len(decoded) != 32 {
		t.Fatalf("token should encode 32 random bytes: length=%d err=%v", len(decoded), err)
	}
	if first == second {
		t.Fatal("independent token generations returned the same value")
	}
	expectedHash := sha256.Sum256([]byte(first))
	if hashToken(first) != hex.EncodeToString(expectedHash[:]) || hashToken(first) == first {
		t.Fatal("token hash should be the stable SHA-256 digest, not the raw token")
	}
}
