package spectest

import (
	"testing"
)

func TestSlashingsMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runSlashingsTests(t, "mainnet")
}
