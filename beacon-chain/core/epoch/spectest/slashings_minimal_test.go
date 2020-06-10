package spectest

import (
	"testing"
)

func TestSlashingsMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runSlashingsTests(t, "minimal")
}
