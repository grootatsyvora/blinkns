package ttl

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

var pattern = regexp.MustCompile(`^(\d+)(m|h|d|w|mo|y)$`)

const (
	minTTL = time.Minute
	maxTTL = 365 * 24 * time.Hour // 1 year
)

// ParseTTL converts a TTL string like "10m", "189h", "20d", "1w", "1mo", "1y"
// into a time.Duration. Returns an error if the format is invalid or out of bounds.
func ParseTTL(s string) (time.Duration, error) {
	matches := pattern.FindStringSubmatch(s)
	if len(matches) != 3 {
		return 0, fmt.Errorf("invalid TTL %q: use a positive integer + unit (m, h, d, w, mo, y) e.g. 30m, 12h, 5d", s)
	}

	n, _ := strconv.Atoi(matches[1])
	unit := matches[2]

	var d time.Duration
	switch unit {
	case "m":
		d = time.Duration(n) * time.Minute
	case "h":
		d = time.Duration(n) * time.Hour
	case "d":
		d = time.Duration(n) * 24 * time.Hour
	case "w":
		d = time.Duration(n) * 7 * 24 * time.Hour
	case "mo":
		d = time.Duration(n) * 30 * 24 * time.Hour
	case "y":
		d = time.Duration(n) * 365 * 24 * time.Hour
	}

	if d < minTTL {
		return 0, fmt.Errorf("TTL %q is below the minimum of 1m", s)
	}
	if d > maxTTL {
		return 0, fmt.Errorf("TTL %q exceeds the maximum of 1y (8760h)", s)
	}

	return d, nil
}
