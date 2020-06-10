package testing

import (
	"testing"
)

func TestSSZStatic_Minimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runSSZStaticTests(t, "minimal")
}
