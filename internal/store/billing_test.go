package store

import "testing"

// The 15% VAT math is the billing document's core invariant: per-line VAT,
// rounded to cents, totals derived — never trusted from the caller.
func TestLineVATMath(t *testing.T) {
	l := LineVAT(3, 199.00)
	if l.VATZAR != 89.55 {
		t.Fatalf("vat: got %v want 89.55 (3×199=597 × 0.15)", l.VATZAR)
	}
	if l.TotalZAR != 686.55 {
		t.Fatalf("total: got %v want 686.55", l.TotalZAR)
	}
	// Rounding: 2×99.99 → subtotal 199.98, vat 29.997 → 30.00
	l2 := LineVAT(2, 99.99)
	if l2.VATZAR != 30.00 {
		t.Fatalf("vat rounding: got %v want 30.00", l2.VATZAR)
	}
}
