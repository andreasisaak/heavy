package herdr

import (
	"strconv"
	"strings"
)

// versionNumbers splits a version such as 1.0.1 into its numbers; valid is false for anything else.
func versionNumbers(version string) (numbers []int, valid bool) {
	for _, part := range strings.Split(version, ".") {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return nil, false
		}
		number, err := strconv.Atoi(part)
		if err != nil {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, true
}

// older reports whether version a comes before version b, number by number; 1.0 comes before 1.0.1.
func older(a, b []int) bool {
	for index := 0; index < len(a) && index < len(b); index++ {
		if a[index] != b[index] {
			return a[index] < b[index]
		}
	}
	return len(a) < len(b)
}
