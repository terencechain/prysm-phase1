package testing

import (
	"testing"
)

func TestGetBeaconFuzzState(t *testing.T) {
	t.Skip("Skipping for phase 1")
	if _, err := GetBeaconFuzzState(1); err != nil {
		t.Fatal(err)
	}
}
