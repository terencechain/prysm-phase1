package spectest

import (
	"testing"
)

func TestVoluntaryExitMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runVoluntaryExitTest(t, "minimal")
}
