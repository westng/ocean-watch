package oceanengine

import (
	"fmt"
	"math"
)

func positiveInt32(value int, field string) (int32, error) {
	if value < 1 || value > math.MaxInt32 {
		return 0, fmt.Errorf("%s must be between 1 and %d", field, math.MaxInt32)
	}
	return int32(value), nil
}
