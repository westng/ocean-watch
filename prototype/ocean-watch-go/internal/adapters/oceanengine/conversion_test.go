package oceanengine

import (
	"math"
	"testing"
)

func TestPositiveInt32RejectsInvalidAndOverflowingValues(t *testing.T) {
	for _, value := range []int{-1, 0} {
		if _, err := positiveInt32(value, "page"); err == nil {
			t.Fatalf("positiveInt32(%d) accepted an invalid value", value)
		}
	}
	if int64(math.MaxInt) > int64(math.MaxInt32) {
		if _, err := positiveInt32(int(int64(math.MaxInt32)+1), "page"); err == nil {
			t.Fatal("positiveInt32 accepted an overflowing value")
		}
	}
	for _, value := range []int{1, math.MaxInt32} {
		converted, err := positiveInt32(value, "page")
		if err != nil || int(converted) != value {
			t.Fatalf("positiveInt32(%d) = %d, %v", value, converted, err)
		}
	}
}
