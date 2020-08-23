package params

import (
	"time"

	"github.com/prysmaticlabs/prysm/shared/bytesutil"
)

// MainnetConfig returns the configuration to be used in the main network.
func MainnetConfig() *BeaconChainConfig {
	return mainnetBeaconConfig
}

// MainnetShardConfig returns the configuration to be used in the main network.
func MainnetShardConfig() *ShardChainConfig {
	return mainnetShardChainConfig
}

// UseMainnetConfig for beacon chain services.
func UseMainnetConfig() {
	beaconConfig = MainnetConfig()
	shardConfig = MainnetShardConfig()
}

var mainnetNetworkConfig = &NetworkConfig{
	GossipMaxSize:                     1 << 20, // 1 MiB
	MaxChunkSize:                      1 << 20, // 1 MiB
	AttestationSubnetCount:            64,
	AttestationPropagationSlotRange:   32,
	RandomSubnetsPerValidator:         1 << 0,
	EpochsPerRandomSubnetSubscription: 1 << 8,
	MaxRequestBlocks:                  1 << 10, // 1024
	TtfbTimeout:                       5 * time.Second,
	RespTimeout:                       10 * time.Second,
	MaximumGossipClockDisparity:       500 * time.Millisecond,
	ETH2Key:                           "eth2",
	AttSubnetKey:                      "attnets",
	ContractDeploymentBlock:           0,
	DepositContractAddress:            "0x", // To be updated once the mainnet contract is deployed.
	ChainID:                           1,    // Chain ID of eth1 mainnet.
	NetworkID:                         1,    // Network ID of eth1 mainnet.
	BootstrapNodes:                    []string{},
}

var mainnetBeaconConfig = &BeaconChainConfig{
	// Constants (Non-configurable)
	FarFutureEpoch:           1<<64 - 1,
	BaseRewardsPerEpoch:      4,
	DepositContractTreeDepth: 32,
	GenesisDelay:             172800, // 2 days

	// Misc constant.
	TargetCommitteeSize:            128,
	MaxValidatorsPerCommittee:      2048,
	MaxCommitteesPerSlot:           64,
	MinPerEpochChurnLimit:          4,
	ChurnLimitQuotient:             1 << 16,
	ShuffleRoundCount:              90,
	MinGenesisActiveValidatorCount: 16384,
	MinGenesisTime:                 0, // Zero until a proper time is decided.
	TargetAggregatorsPerCommittee:  16,
	HysteresisQuotient:             4,
	HysteresisDownwardMultiplier:   1,
	HysteresisUpwardMultiplier:     5,

	// Gwei value constants.
	MinDepositAmount:          1 * 1e9,
	MaxEffectiveBalance:       32 * 1e9,
	EjectionBalance:           16 * 1e9,
	EffectiveBalanceIncrement: 1 * 1e9,

	// Initial value constants.
	BLSWithdrawalPrefixByte: byte(0),
	ZeroHash:                [32]byte{},

	// Time parameter constants.
	MinAttestationInclusionDelay:     1,
	SecondsPerSlot:                   12,
	SlotsPerEpoch:                    32,
	MinSeedLookahead:                 1,
	MaxSeedLookahead:                 4,
	EpochsPerEth1VotingPeriod:        32,
	SlotsPerHistoricalRoot:           8192,
	MinValidatorWithdrawabilityDelay: 256,
	ShardCommitteePeriod:             256,
	MinEpochsToInactivityPenalty:     4,
	Eth1FollowDistance:               1024,
	SafeSlotsToUpdateJustified:       8,
	SecondsPerETH1Block:              14,

	// State list length constants.
	EpochsPerHistoricalVector: 65536,
	EpochsPerSlashingsVector:  8192,
	HistoricalRootsLimit:      16777216,
	ValidatorRegistryLimit:    1099511627776,

	// Reward and penalty quotients constants.
	BaseRewardFactor:            64,
	WhistleBlowerRewardQuotient: 512,
	ProposerRewardQuotient:      8,
	InactivityPenaltyQuotient:   1 << 24,
	MinSlashingPenaltyQuotient:  32,

	// Max operations per block constants.
	MaxProposerSlashings: 16,
	MaxAttesterSlashings: 2,
	MaxAttestations:      128,
	MaxDeposits:          16,
	MaxVoluntaryExits:    16,

	// BLS domain values.
	DomainBeaconProposer:    bytesutil.ToBytes4(bytesutil.Bytes4(0)),
	DomainBeaconAttester:    bytesutil.ToBytes4(bytesutil.Bytes4(1)),
	DomainRandao:            bytesutil.ToBytes4(bytesutil.Bytes4(2)),
	DomainDeposit:           bytesutil.ToBytes4(bytesutil.Bytes4(3)),
	DomainVoluntaryExit:     bytesutil.ToBytes4(bytesutil.Bytes4(4)),
	DomainSelectionProof:    bytesutil.ToBytes4(bytesutil.Bytes4(5)),
	DomainAggregateAndProof: bytesutil.ToBytes4(bytesutil.Bytes4(6)),

	// Prysm constants.
	GweiPerEth:                1000000000,
	BLSSecretKeyLength:        32,
	BLSPubkeyLength:           48,
	BLSSignatureLength:        96,
	DefaultBufferSize:         10000,
	WithdrawalPrivkeyFileName: "/shardwithdrawalkey",
	ValidatorPrivkeyFileName:  "/validatorprivatekey",
	RPCSyncCheck:              1,
	EmptySignature:            [96]byte{},
	DefaultPageSize:           250,
	MaxPeersToSync:            15,
	SlotsPerArchivedPoint:     2048,
	GenesisCountdownInterval:  time.Minute,

	// Slasher related values.
	WeakSubjectivityPeriod:    54000,
	PruneSlasherStoragePeriod: 10,

	// Fork related values.
	GenesisForkVersion:  []byte{0, 0, 0, 0},
	NextForkVersion:     []byte{0, 0, 0, 0}, // Set to GenesisForkVersion unless there is a scheduled fork
	NextForkEpoch:       1<<64 - 1,          // Set to FarFutureEpoch unless there is a scheduled fork.
	ForkVersionSchedule: map[uint64][]byte{
		// Any further forks must be specified here by their epoch number.
	},
}

type ShardChainConfig struct {
	MaxShard                        uint64   // MaxShard defines the max shard allowed to crosslink with beacon chain.
	MaxShardBlockSize               uint64   // MaxShardBlockSize defines the max shard block size.
	TargetShardBlockSize            uint64   // TargetShardBlockSize defines the target shard block size.
	DomainShardProposal             [4]byte  // DomainShardProposal defines the BLS signature domain for shard proposal.
	DomainShardCommittee            [4]byte  // DomainShardCommittee defines the BLS signature domain for shard committee.
	DomainLightClient               [4]byte  // DomainLightClient defines the BLS signature domain for light client.
	DomainCustodyBitSlashing        [4]byte  // DomainCustodyBitSlashing defines the custody bit slashing.
	Phase1GenesisSlot               uint64   // Phase1GenesisSlot defines the slot when phase 1 genesis
	GasPriceAdjustmentCoefficient   uint64   // GasPriceAdjustmentCoefficient defines the gas price adjustment coefficient.
	MaxGasPrice                     uint64   // MaxGasPrice defines the max gas price.
	MinGasPrice                     uint64   // MinGasPrice defines the min gas price.
	ShardCommitteePeriod            uint64   // ShardCommitteePeriod defines the shard committee period.
	ShardBlockOffsets               []uint64 // ShardBlockOffsets defines the shard block offsets.
	OnlineCountDown                 uint64   // OnlineCountDown defines the default count down start number.
	LightClientCommitteeSize        uint64   // LightClientCommitteeSize defines the light client committee size.
	LightClientCommitteePeriod      uint64   // LightClientCommitteePeriod defines the light client committee period.
	MaxChunkChallengeDelay          uint64   // MaxChunkChallengeDelay defines the max chunk challenge delay.
	MinorRewardQuotient             uint64   // MinorRewardQuotient defines the minor reward quotient.
	MaxCustodyChunkChallengeRecords uint64   // MaxCustodyChunkChallengeRecords defines the max custody chunk challenge records.
	EpochsPerCustodyPeriod          uint64   // EpochsPerCustodyPeriod defines how many epochs per custody period.
	InitialActiveShards             uint64   // InitialActiveShards defines the initial active shard count.
}

var mainnetShardChainConfig = &ShardChainConfig{
	MaxShard:                        64,
	InitialActiveShards:             64,
	MaxShardBlockSize:               1 << 20,
	TargetShardBlockSize:            1 << 18,
	DomainShardProposal:             bytesutil.ToBytes4(bytesutil.Bytes4(128)),
	DomainShardCommittee:            bytesutil.ToBytes4(bytesutil.Bytes4(129)),
	DomainLightClient:               bytesutil.ToBytes4(bytesutil.Bytes4(130)),
	DomainCustodyBitSlashing:        bytesutil.ToBytes4(bytesutil.Bytes4(131)),
	GasPriceAdjustmentCoefficient:   8,
	MaxGasPrice:                     16384,
	MinGasPrice:                     8,
	ShardCommitteePeriod:            256,
	ShardBlockOffsets:               []uint64{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233},
	OnlineCountDown:                 8,
	LightClientCommitteeSize:        128,
	LightClientCommitteePeriod:      256,
	MaxChunkChallengeDelay:          1 << 15,
	MinorRewardQuotient:             256,
	MaxCustodyChunkChallengeRecords: 16, // TODO(0): Stubbed to be replaced by the real value.
	EpochsPerCustodyPeriod:          16384,
}
