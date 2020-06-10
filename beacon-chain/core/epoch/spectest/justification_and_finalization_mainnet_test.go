package spectest

import (
	"testing"
)

func TestJustificationAndFinalizationMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runJustificationAndFinalizationTests(t, "mainnet")
}
