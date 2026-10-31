package cmd

import "testing"

func TestFormatCostShowsNOK(t *testing.T) {
	if got := formatCost(12.34); got != "$12.34 (~130 NOK)" {
		t.Errorf("formatCost = %q", got)
	}
}
