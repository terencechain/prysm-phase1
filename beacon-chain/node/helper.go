package node

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Given input string `block_root:epoch_number`, this verifies the input string is valid, and
// returns the block root as bytes and epoch number as unsigned integers.
func convertWspInput(wsp string) ([]byte, uint64, error) {
	if wsp == "" {
		return nil, 0, nil
	}

	// Weak subjectivity input string must contain ":" to separate epoch and block root.
	if !strings.Contains(wsp, ":") {
		return nil, 0, fmt.Errorf("%s did not contain column", wsp)
	}

	// Strip prefix "0x" if it's part of the input string.
	if strings.HasPrefix(wsp, "0x") {
		wsp = wsp[2:]
	}

	// Get the hexadecimal block root from input string.
	s := strings.Split(wsp, ":")
	if len(s) != 2 {
		return nil, 0, errors.New("weak subjectivity checkpoint input should be in `block_root:epoch_number` format")
	}

	bRoot, err := hex.DecodeString(s[0])
	if err != nil {
		return nil, 0, err
	}
	if len(bRoot) != 32 {
		return nil, 0, errors.New("block root is not length of 32")
	}

	// Get the epoch number from input string.
	epoch, err := strconv.ParseUint(s[1], 10, 64)
	if err != nil {
		return nil, 0, err
	}

	return bRoot, epoch, nil
}
