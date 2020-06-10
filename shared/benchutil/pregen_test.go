package benchutil

import (
	"testing"
)

func TestPreGenFullBlock(t *testing.T) {
	t.Skip("Skipping for phase 1")
	_, err := PreGenFullBlock()
	if err != nil {
		t.Fatal(err)
	}
}

func TestPreGenState1Epoch(t *testing.T) {
	t.Skip("Skipping for phase 1")
	_, err := PreGenFullBlock()
	if err != nil {
		t.Fatal(err)
	}
}

func TestPreGenState2FullEpochs(t *testing.T) {
	t.Skip("Skipping for phase 1")
	_, err := PreGenFullBlock()
	if err != nil {
		t.Fatal(err)
	}
}
