package spectest

import (
	"testing"
)

func TestBlockHeaderMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runBlockHeaderTest(t, "minimal")
}
