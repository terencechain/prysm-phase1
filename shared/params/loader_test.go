package params

import (
	"io/ioutil"
	"path"
	"strings"
	"testing"

	"github.com/bazelbuild/rules_go/go/tools/bazel"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestLoadConfigFile(t *testing.T) {
	mainnetConfigFile := ConfigFilePath(t, "mainnet")
	LoadChainConfigFile(mainnetConfigFile)
	if BeaconConfig().MaxCommitteesPerSlot != MainnetConfig().MaxCommitteesPerSlot {
		t.Errorf("Expected MaxCommitteesPerSlot to be set to mainnet value: %d found: %d",
			MainnetConfig().MaxCommitteesPerSlot,
			BeaconConfig().MaxCommitteesPerSlot)
	}
	if BeaconConfig().SecondsPerSlot != MainnetConfig().SecondsPerSlot {
		t.Errorf("Expected SecondsPerSlot to be set to mainnet value: %d found: %d",
			MainnetConfig().SecondsPerSlot,
			BeaconConfig().SecondsPerSlot)
	}
	minimalConfigFile := ConfigFilePath(t, "minimal")
	LoadChainConfigFile(minimalConfigFile)
	if BeaconConfig().MaxCommitteesPerSlot != BeaconMinimalSpecConfig().MaxCommitteesPerSlot {
		t.Errorf("Expected MaxCommitteesPerSlot to be set to minimal value: %d found: %d",
			BeaconMinimalSpecConfig().MaxCommitteesPerSlot,
			BeaconConfig().MaxCommitteesPerSlot)
	}
	if BeaconConfig().SecondsPerSlot != BeaconMinimalSpecConfig().SecondsPerSlot {
		t.Errorf("Expected SecondsPerSlot to be set to minimal value: %d found: %d",
			BeaconMinimalSpecConfig().SecondsPerSlot,
			BeaconConfig().SecondsPerSlot)
	}
}

func TestLoadConfigFile_OverwriteCorrectly(t *testing.T) {
	file, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	// Set current config to minimal config
	OverrideBeaconConfig(BeaconMinimalSpecConfig())

	// load empty config file, so that it defaults to mainnet values
	LoadChainConfigFile(file.Name())
	if BeaconConfig().MinGenesisTime != MainnetConfig().MinGenesisTime {
		t.Errorf("Expected MinGenesisTime to be set to mainnet value: %d found: %d",
			MainnetConfig().MinGenesisTime,
			BeaconConfig().MinGenesisTime)
	}
	if BeaconConfig().SlotsPerEpoch != MainnetConfig().SlotsPerEpoch {
		t.Errorf("Expected SlotsPerEpoch to be set to mainnet value: %d found: %d",
			MainnetConfig().SlotsPerEpoch,
			BeaconConfig().SlotsPerEpoch)
	}
}

func Test_replaceHexStringWithYAMLFormat(t *testing.T) {

	testLines := []struct {
		line   string
		wanted string
	}{
		{
			line:   "ONE_BYTE: 0x41",
			wanted: "ONE_BYTE: 65\n",
		},
		{
			line:   "FOUR_BYTES: 0x41414141",
			wanted: "FOUR_BYTES: \n- 65\n- 65\n- 65\n- 65\n",
		},
		{
			line:   "THREE_BYTES: 0x414141",
			wanted: "THREE_BYTES: \n- 65\n- 65\n- 65\n- 0\n",
		},
		{
			line:   "EIGHT_BYTES: 0x4141414141414141",
			wanted: "EIGHT_BYTES: \n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n",
		},
		{
			line: "SIXTEEN_BYTES: 0x41414141414141414141414141414141",
			wanted: "SIXTEEN_BYTES: \n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n",
		},
		{
			line: "TWENTY_BYTES: 0x4141414141414141414141414141414141414141",
			wanted: "TWENTY_BYTES: \n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n",
		},
		{
			line: "THIRTY_TWO_BYTES: 0x4141414141414141414141414141414141414141414141414141414141414141",
			wanted: "THIRTY_TWO_BYTES: \n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n",
		},
		{
			line: "FORTY_EIGHT_BYTES: 0x41414141414141414141414141414141414141414141414141414141414141414141" +
				"4141414141414141414141414141",
			wanted: "FORTY_EIGHT_BYTES: \n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n",
		},
		{
			line: "NINETY_SIX_BYTES: 0x414141414141414141414141414141414141414141414141414141414141414141414141" +
				"4141414141414141414141414141414141414141414141414141414141414141414141414141414141414141414141" +
				"41414141414141414141414141",
			wanted: "NINETY_SIX_BYTES: \n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n" +
				"- 65\n- 65\n- 65\n- 65\n- 65\n- 65\n",
		},
	}
	for _, line := range testLines {
		parts := replaceHexStringWithYAMLFormat(line.line)
		res := strings.Join(parts, "\n")

		if res != line.wanted {
			t.Errorf("expected conversion to be: %v got: %v", line.wanted, res)
		}
	}
}

// ConfigFilePath sets the proper config and returns the relevant
// config file path from eth2-spec-tests directory.
func ConfigFilePath(t *testing.T, config string) string {
	configFolderPath := path.Join("tests", config)
	filepath, err := bazel.Runfile(configFolderPath)
	require.NoError(t, err)
	configFilePath := path.Join(filepath, "config", "phase0.yaml")
	return configFilePath
}
