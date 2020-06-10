package testing

import (
	"testing"
)

func TestSZZStatic_Mainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runSSZStaticTests(t, "mainnet")
}
