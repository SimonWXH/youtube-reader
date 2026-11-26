package ui

import "testing"

func TestThumbMaxSideUses2xRetina(t *testing.T) {
	if got := thumbMaxSide(0); got != 640 {
		t.Fatalf("thumbMaxSide(0) = %d, want 640", got)
	}
	if got := thumbMaxSide(320); got != 640 {
		t.Fatalf("thumbMaxSide(320) = %d, want 640 (2x Retina)", got)
	}
	if got := thumbMaxSide(400); got != 800 {
		t.Fatalf("thumbMaxSide(400) = %d, want 800 (2x Retina)", got)
	}
}
