package spectest

import (
	"testing"
)

func TestJustificationAndFinalizationMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runJustificationAndFinalizationTests(t, "minimal")
}
