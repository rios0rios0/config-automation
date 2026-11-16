package entities_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rios0rios0/config-automation/internal/domain/entities"
)

// fragment returns the name and content `chlog new` would write at stamp.
func fragment(stamp time.Time) (string, string) {
	return fmt.Sprintf(".changes/unreleased/%d-changed.yaml", stamp.UnixNano()),
		"kind: Changed\nbody: refreshed the guidance\ntime: " + stamp.Format(time.RFC3339Nano) + "\n"
}

func TestValidateChlogFragment(t *testing.T) {
	t.Parallel()

	t.Run("should accept a fragment chlog new wrote", func(t *testing.T) {
		t.Parallel()
		// given
		now := time.Now()
		file, content := fragment(now)

		// when
		err := entities.ValidateChlogFragment(file, content, now)

		// then
		require.NoError(t, err)
	})

	t.Run("should accept a quoted time", func(t *testing.T) {
		t.Parallel()
		// given
		now := time.Now()
		file, _ := fragment(now)
		content := "kind: Changed\nbody: refreshed\ntime: '" + now.Format(time.RFC3339Nano) + "'\n"

		// when
		err := entities.ValidateChlogFragment(file, content, now)

		// then
		require.NoError(t, err)
	})

	t.Run("should reject a fragment chlog new could not have written", func(t *testing.T) {
		t.Parallel()
		now := time.Now()
		file, content := fragment(now)
		for name, test := range map[string]struct {
			file, content, message string
		}{
			"a file that is not YAML": {
				".changes/unreleased/note.md", content, "not a .yaml file",
			},
			"a fragment without a body": {
				file, "kind: Changed\ntime: " + now.Format(time.RFC3339Nano) + "\n", "missing a kind:, body: or time: line",
			},
			"a hand-picked filename": {
				".changes/unreleased/refresh.yaml", content, "the filename is not the nanosecond prefix chlog new writes",
			},
			"a time that does not parse": {
				file, "kind: Changed\nbody: refreshed\ntime: yesterday\n", "time 'yesterday' does not parse",
			},
			"a filename and a time that disagree": {
				file, "kind: Changed\nbody: refreshed\ntime: " + now.Add(-time.Minute).Format(time.RFC3339Nano) + "\n",
				"filename and time disagree, so chlog new did not write it",
			},
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				// given / when
				err := entities.ValidateChlogFragment(test.file, test.content, now)

				// then
				require.Error(t, err)
				assert.Equal(t, test.message, err.Error())
			})
		}
	})

	t.Run("should reject a fragment stamped in the future", func(t *testing.T) {
		t.Parallel()
		// given
		now := time.Now()
		file, content := fragment(now.Add(2 * time.Minute))

		// when
		err := entities.ValidateChlogFragment(file, content, now)

		// then
		require.Error(t, err)
		assert.Equal(t, "time is in the future", err.Error())
	})
}
