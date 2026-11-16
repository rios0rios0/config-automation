package entities

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	// fragmentNanosecondDigits is the length of the nanosecond timestamp that
	// opens the file name `chlog new` gives a fragment.
	fragmentNanosecondDigits = 19
	// fragmentClockTolerance allows for the two clock reads behind a fragment
	// straddling a second boundary.
	fragmentClockTolerance = 2 * time.Second
	// fragmentFutureTolerance allows for clock skew between the runner and
	// the moment the fragment was written.
	fragmentFutureTolerance = 60 * time.Second
	// fragmentTimeKey opens the line that holds a fragment's time.
	fragmentTimeKey = "time: "
)

// ValidateChlogFragment checks that a new fragment is one `chlog new` wrote.
// A fragment `chlog batch` cannot parse breaks the repository's next release,
// which is far from here and hard to trace back, so it is rejected now. The
// other half is provenance: `chlog new` stamps `time` and names the file from
// two clock reads microseconds apart, so the two agree to the second on every
// fragment the tool wrote and on none typed by hand. A time in the future is
// rejected outright, because `chlog batch` orders a release section by it.
func ValidateChlogFragment(file, content string, now time.Time) error {
	if !strings.HasSuffix(file, ".yaml") {
		return errors.New("not a .yaml file")
	}
	for _, key := range []string{"kind: ", "body: ", fragmentTimeKey} {
		if !hasFilledLine(content, key) {
			return errors.New("missing a kind:, body: or time: line")
		}
	}
	prefix, _, _ := strings.Cut(path.Base(file), "-")
	nanoseconds, err := strconv.ParseInt(prefix, 10, 64)
	if len(prefix) != fragmentNanosecondDigits || err != nil || nanoseconds < 0 {
		return errors.New("the filename is not the nanosecond prefix chlog new writes")
	}
	value := fragmentTime(content)
	stamped, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fmt.Errorf("time '%s' does not parse", value)
	}
	// Whole seconds are compared, as the two clock reads are a second apart at
	// most when they straddle a boundary.
	gap := time.Duration(abs(time.Unix(0, nanoseconds).Unix()-stamped.Unix())) * time.Second
	if gap > fragmentClockTolerance {
		return errors.New("filename and time disagree, so chlog new did not write it")
	}
	if stamped.Unix() > now.Add(fragmentFutureTolerance).Unix() {
		return errors.New("time is in the future")
	}
	return nil
}

// abs returns the magnitude of a whole number of seconds.
func abs(seconds int64) int64 {
	if seconds < 0 {
		return -seconds
	}
	return seconds
}

// hasFilledLine reports whether a line starts with key and carries a value.
func hasFilledLine(content, key string) bool {
	for line := range strings.SplitSeq(content, "\n") {
		if strings.HasPrefix(line, key) && len(line) > len(key) {
			return true
		}
	}
	return false
}

// fragmentTime returns the value of the first time line, unquoted.
func fragmentTime(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		if value, found := strings.CutPrefix(line, fragmentTimeKey); found {
			return strings.Trim(strings.TrimSpace(value), `'"`)
		}
	}
	return ""
}
