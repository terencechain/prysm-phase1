package spectest

import (
	"testing"
)

func TestBlockHeaderMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runBlockHeaderTest(t, "mainnet")
}
