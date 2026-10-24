package mysql

import (
	"context"
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
