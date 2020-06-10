package spectest

import (
	"testing"
)

func TestBlockProcessingMainnetYaml(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runBlockProcessingTest(t, "mainnet")
}
