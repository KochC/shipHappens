package calc

import "testing"

func TestAdd(t *testing.T) {
	if Add(2, 3) != 5 {
		t.Fatal("2+3 should be 5")
	}
}

func TestMax(t *testing.T) {
	if Max(2, 9) != 9 || Max(9, 2) != 9 {
		t.Fatal("Max wrong")
	}
}

func TestSum(t *testing.T) {
	if Sum([]int{1, 2, 3, 4}) != 10 {
		t.Fatalf("Sum wrong: %d", Sum([]int{1, 2, 3, 4}))
	}
	if Sum(nil) != 0 {
		t.Fatal("Sum(nil) should be 0")
	}
}
