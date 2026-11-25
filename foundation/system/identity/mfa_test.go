package identity

import "testing"

func TestTOTPReferenceVectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, vector := range []struct {
		seconds int64
		code    string
	}{{59, "287082"}, {1111111109, "081804"}, {1234567890, "005924"}, {2000000000, "279037"}} {
		if got := totpAt(secret, vector.seconds/30); got != vector.code {
			t.Fatalf("counter at %d: got %s, want %s", vector.seconds, got, vector.code)
		}
	}
}
