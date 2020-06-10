package spectest

import (
	"testing"
)

func TestVoluntaryExitMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runVoluntaryExitTest(t, "mainnet")
}
