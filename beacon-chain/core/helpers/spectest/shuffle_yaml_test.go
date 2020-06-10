// Package spectest contains all conformity specification tests
// for validator shuffling logic according to the eth2 beacon spec.
package spectest

import (
	"encoding/hex"
	"fmt"
	"path"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/go-yaml/yaml"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/params/spectest"
	"github.com/prysmaticlabs/prysm/shared/testutil"
)

func TestShufflingMinimal(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runShuffleTests(t, "minimal")
}

func TestShufflingMainnet(t *testing.T) {
	t.Skip("Skipping for phase 1")
	runShuffleTests(t, "mainnet")
}

func runShuffleTests(t *testing.T, config string) {
	if err := spectest.SetConfig(t, config); err != nil {
		t.Fatal(err)
	}

	testFolders, testsFolderPath := testutil.TestFolders(t, config, "shuffling/core/shuffle")
	for _, folder := range testFolders {
		t.Run(folder.Name(), func(t *testing.T) {
			testCaseFile, err := testutil.BazelFileBytes(path.Join(testsFolderPath, folder.Name(), "mapping.yaml"))
			if err != nil {
				t.Fatalf("could not read YAML tests directory: %v", err)
			}

			testCase := &ShuffleTestCase{}
			if err := yaml.Unmarshal(testCaseFile, testCase); err != nil {
				t.Fatalf("could not unmarshal YAML file into test struct: %v", err)
			}
			if err := runShuffleTest(testCase); err != nil {
				t.Fatalf("shuffle test failed: %v", err)
			}
		})
	}
}

// RunShuffleTest uses validator set specified from a YAML file, runs the validator shuffle
// algorithm, then compare the output with the expected output from the YAML file.
func runShuffleTest(testCase *ShuffleTestCase) error {
	baseSeed, err := hex.DecodeString(testCase.Seed[2:])
	if err != nil {
		return err
	}

	seed := common.BytesToHash(baseSeed)
	testIndices := make([]uint64, testCase.Count, testCase.Count)
	for i := uint64(0); i < testCase.Count; i++ {
		testIndices[i] = i
	}
	shuffledList := make([]uint64, testCase.Count)
	for i := uint64(0); i < testCase.Count; i++ {
		si, err := helpers.ShuffledIndex(i, testCase.Count, seed)
		if err != nil {
			return err
		}
		shuffledList[i] = si
	}
	if !reflect.DeepEqual(shuffledList, testCase.Mapping) {
		return fmt.Errorf("shuffle result error: expected %v, actual %v", testCase.Mapping, shuffledList)
	}
	return nil
}
