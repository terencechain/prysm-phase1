package endtoend

import (
	"os"
	"strconv"
	"testing"

	e2eParams "github.com/prysmaticlabs/prysm/endtoend/params"

	ev "github.com/prysmaticlabs/prysm/endtoend/evaluators"
	"github.com/prysmaticlabs/prysm/endtoend/types"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
)

func TestEndToEnd_Long_MinimalConfig(t *testing.T) {
	testutil.ResetCache()
	params.UseE2EConfig()
	if err := e2eParams.Init(e2eParams.LongRunningBeaconCount); err != nil {
		t.Fatal(err)
	}

	epochsToRun := 20
	var err error
	epochStr, ok := os.LookupEnv("E2E_EPOCHS")
	if ok {
		epochsToRun, err = strconv.Atoi(epochStr)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		t.Skip("E2E_EPOCHS not set")
	}

	minimalConfig := &types.E2EConfig{
		BeaconFlags:    []string{},
		ValidatorFlags: []string{},
		EpochsToRun:    uint64(epochsToRun),
		TestSync:       false,
		TestDeposits:   true,
		TestSlasher:    true,
		Evaluators: []types.Evaluator{
			ev.PeersConnect,
			ev.HealthzCheck,
			ev.MetricsCheck,
			ev.ValidatorsAreActive,
			ev.ValidatorsParticipating,
			ev.FinalizationOccurs,
			ev.ProcessesDepositedValidators,
			ev.ProposeVoluntaryExit,
			ev.DepositedValidatorsAreActive,
			ev.ValidatorHasExited,
		},
	}

	runEndToEndTest(t, minimalConfig)
}
