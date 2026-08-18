// Package calc provides small arithmetic helpers, used to demo a Ship Happens
// Go CI pipeline.
package calc

// Add returns the sum of two integers.
func Add(a, b int) int { return a + b }

// Max returns the larger of two integers.
func Max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// Sum returns the total of a slice of integers.
func Sum(xs []int) int {
	total := 0
	for _, x := range xs {
		total += x
	}
	return total
}
