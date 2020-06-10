package spectest

import (
	"testing"
)

func TestDepositMinimalYaml(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runDepositTest(t, "minimal")
}
