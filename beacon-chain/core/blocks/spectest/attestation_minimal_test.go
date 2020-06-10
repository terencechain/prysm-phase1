package spectest

import (
	"testing"
)

func TestAttestationMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runAttestationTest(t, "minimal")
}
