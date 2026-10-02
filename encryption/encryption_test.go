package encryption

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
)

func TestRoundTripWithHexBase64AndRawKeys(t *testing.T) {
	hexKey, err := GenerateKey256()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := hex.DecodeString(hexKey)
	keys := map[string]string{
		"hex":     hexKey,
		"base64":  base64.StdEncoding.EncodeToString(raw),
		"raw16":   "0123456789abcde!", // not hex, not base64: used verbatim
		"alnum16": "0123456789abcdeZ", // valid base64 of 12 bytes: falls back to raw
	}
	for name, key := range keys {
		t.Run(name, func(t *testing.T) {
			e, err := New(key)
			if err != nil {
				t.Fatal(err)
			}
			ct, err := e.EncryptString("secret message")
			if err != nil {
				t.Fatal(err)
			}
			pt, err := e.DecryptString(ct)
			if err != nil || pt != "secret message" {
				t.Fatalf("round trip = %q, %v", pt, err)
			}
		})
	}
}

func TestEncryptIsRandomized(t *testing.T) {
	e := MustNew(mustKey(t))
	a, _ := e.Encrypt([]byte("same"))
	b, _ := e.Encrypt([]byte("same"))
	if bytes.Equal(a, b) {
		t.Fatal("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestDecryptRejectsTamperingWrongKeyAndShortInput(t *testing.T) {
	e := MustNew(mustKey(t))
	ct, _ := e.Encrypt([]byte("payload"))

	tampered := append([]byte(nil), ct...)
	tampered[len(tampered)-1] ^= 0xff
	if _, err := e.Decrypt(tampered); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("tampered ciphertext: %v, want ErrDecryptFailed", err)
	}
	other := MustNew(mustKey(t))
	if _, err := other.Decrypt(ct); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("wrong key: %v, want ErrDecryptFailed", err)
	}
	if _, err := e.Decrypt([]byte("short")); !errors.Is(err, ErrInvalidCiphertext) {
		t.Fatalf("short input: %v, want ErrInvalidCiphertext", err)
	}
	if _, err := e.DecryptString("%%% not base64"); err == nil {
		t.Fatal("invalid base64 should fail")
	}
}

func TestInvalidKeysAndSizes(t *testing.T) {
	if _, err := New("short"); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("New(short) = %v, want ErrInvalidKey", err)
	}
	if _, err := GenerateKey(20); err == nil {
		t.Fatal("GenerateKey(20) should fail")
	}
	for _, n := range []int{16, 24, 32} {
		k, err := GenerateKey(n)
		if err != nil || len(k) != 2*n {
			t.Fatalf("GenerateKey(%d) = %q, %v", n, k, err)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustNew should panic on a bad key")
		}
	}()
	MustNew("bad")
}

func TestDeterministicEncryption(t *testing.T) {
	e := MustNew(mustKey(t))
	a, _ := e.EncryptDeterministicUNSAFE([]byte("x"))
	b, _ := e.EncryptDeterministicUNSAFE([]byte("x"))
	if !bytes.Equal(a, b) {
		t.Fatal("deterministic encryption should be stable")
	}
	if pt, err := e.Decrypt(a); err != nil || string(pt) != "x" {
		t.Fatalf("deterministic ciphertext should decrypt: %q, %v", pt, err)
	}
}

func mustKey(t *testing.T) string {
	t.Helper()
	k, err := GenerateKey256()
	if err != nil {
		t.Fatal(err)
	}
	return k
}
