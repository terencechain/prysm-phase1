package spectest

import (
	"testing"
)

func TestDepositMainnetYaml(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runDepositTest(t, "mainnet")
}
