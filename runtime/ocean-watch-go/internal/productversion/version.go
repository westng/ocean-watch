// Package productversion owns the product ordering used by release channels.
// Build metadata is deliberately excluded from precedence.
package productversion

import (
	"errors"
	"regexp"
	"strconv"
)

var pattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-(alpha|beta|rc)\.([1-9][0-9]*))?(?:\+codex\.[A-Za-z0-9][A-Za-z0-9.-]*)?$`)

type Version struct {
	Order [5]int
}

func Parse(value string) (Version, error) {
	match := pattern.FindStringSubmatch(value)
	if match == nil {
		return Version{}, errors.New("invalid Ocean Watch product version")
	}
	var result Version
	for i := 0; i < 3; i++ {
		n, err := strconv.ParseInt(match[i+1], 10, 32)
		if err != nil {
			return Version{}, errors.New("product version number is too large")
		}
		result.Order[i] = int(n)
	}
	result.Order[3] = map[string]int{"alpha": 0, "beta": 1, "rc": 2, "": 3}[match[4]]
	if match[5] != "" {
		n, err := strconv.ParseInt(match[5], 10, 32)
		if err != nil {
			return Version{}, errors.New("prerelease number is too large")
		}
		result.Order[4] = int(n)
	}
	return result, nil
}

func (v Version) Prerelease() bool { return v.Order[3] < 3 }

func Compare(left, right string) (int, error) {
	a, err := Parse(left)
	if err != nil {
		return 0, err
	}
	b, err := Parse(right)
	if err != nil {
		return 0, err
	}
	for i := range a.Order {
		if a.Order[i] < b.Order[i] {
			return -1, nil
		}
		if a.Order[i] > b.Order[i] {
			return 1, nil
		}
	}
	return 0, nil
}
