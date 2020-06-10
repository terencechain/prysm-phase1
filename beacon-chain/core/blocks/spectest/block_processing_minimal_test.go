package spectest

import (
	"testing"
)

func TestBlockProcessingMinimalYaml(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runBlockProcessingTest(t, "minimal")
}
