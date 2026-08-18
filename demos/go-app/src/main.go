// Command demo builds a tiny CLI over the calc package.
package main

import (
	"fmt"

	"example.com/calcdemo/calc"
)

func main() {
	nums := []int{3, 1, 4, 1, 5, 9, 2, 6}
	fmt.Printf("sum(%v) = %d\n", nums, calc.Sum(nums))
	fmt.Printf("add(2,3) = %d\n", calc.Add(2, 3))
	fmt.Printf("max(2,9) = %d\n", calc.Max(2, 9))
}
