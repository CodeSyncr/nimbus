package hash

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func TestMakeAndCheck(t *testing.T) {
	h, err := MakeWithCost("hunter2", bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$2") {
		t.Fatalf("not a bcrypt hash: %q", h)
	}
	if !Check("hunter2", h) {
		t.Fatal("correct password rejected")
	}
	if Check("hunter3", h) {
		t.Fatal("wrong password accepted")
	}
	if Check("hunter2", "not-a-hash") {
		t.Fatal("garbage hash accepted")
	}
}

func TestMakeUsesDefaultCostAndClampsCost(t *testing.T) {
	h, err := Make("pw")
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := bcrypt.Cost([]byte(h)); c != DefaultCost {
		t.Fatalf("cost = %d, want %d", c, DefaultCost)
	}
	h, err = MakeWithCost("pw", 1)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := bcrypt.Cost([]byte(h)); c != bcrypt.MinCost {
		t.Fatalf("cost below minimum = %d, want %d", c, bcrypt.MinCost)
	}
}
