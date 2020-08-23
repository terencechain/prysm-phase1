package params

import "github.com/prysmaticlabs/prysm/shared/bytesutil"

// UseBeaconMinimalConfig for beacon chain services.
func UseBeaconMinimalConfig() {
	beaconConfig = BeaconMinimalSpecConfig()
}

// UseShardMinimalConfig for beacon chain services.
func UseShardMinimalConfig() {
	shardConfig = ShardMinimalSpecConfig()
}

// BeaconMinimalSpecConfig retrieves the minimal config used in spec tests.
func BeaconMinimalSpecConfig() *BeaconChainConfig {
	minimalBeaconConfig := mainnetBeaconConfig.Copy()

	// Misc
	minimalBeaconConfig.MaxCommitteesPerSlot = 4
	minimalBeaconConfig.TargetCommitteeSize = 4
	minimalBeaconConfig.MaxValidatorsPerCommittee = 2048
	minimalBeaconConfig.MinPerEpochChurnLimit = 4
	minimalBeaconConfig.ChurnLimitQuotient = 65536
	minimalBeaconConfig.ShuffleRoundCount = 10
	minimalBeaconConfig.MinGenesisActiveValidatorCount = 64
	minimalBeaconConfig.MinGenesisTime = 0
	minimalBeaconConfig.GenesisDelay = 300 // 5 minutes
	minimalBeaconConfig.TargetAggregatorsPerCommittee = 3

	// Gwei values
	minimalBeaconConfig.MinDepositAmount = 1e9
	minimalBeaconConfig.MaxEffectiveBalance = 32e9
	minimalBeaconConfig.EjectionBalance = 16e9
	minimalBeaconConfig.EffectiveBalanceIncrement = 1e9

	// Initial values
	minimalBeaconConfig.BLSWithdrawalPrefixByte = byte(0)

	// Time parameters
	minimalBeaconConfig.SecondsPerSlot = 6
	minimalBeaconConfig.MinAttestationInclusionDelay = 1
	minimalBeaconConfig.SlotsPerEpoch = 8
	minimalBeaconConfig.MinSeedLookahead = 1
	minimalBeaconConfig.MaxSeedLookahead = 4
	minimalBeaconConfig.EpochsPerEth1VotingPeriod = 4
	minimalBeaconConfig.SlotsPerHistoricalRoot = 64
	minimalBeaconConfig.MinValidatorWithdrawabilityDelay = 256
	minimalBeaconConfig.ShardCommitteePeriod = 64
	minimalBeaconConfig.MinEpochsToInactivityPenalty = 4
	minimalBeaconConfig.Eth1FollowDistance = 16
	minimalBeaconConfig.SafeSlotsToUpdateJustified = 2
	minimalBeaconConfig.SecondsPerETH1Block = 14

	// State vector lengths
	minimalBeaconConfig.EpochsPerHistoricalVector = 64
	minimalBeaconConfig.EpochsPerSlashingsVector = 64
	minimalBeaconConfig.HistoricalRootsLimit = 16777216
	minimalBeaconConfig.ValidatorRegistryLimit = 1099511627776

	// Reward and penalty quotients
	minimalBeaconConfig.BaseRewardFactor = 64
	minimalBeaconConfig.WhistleBlowerRewardQuotient = 512
	minimalBeaconConfig.ProposerRewardQuotient = 8
	minimalBeaconConfig.InactivityPenaltyQuotient = 1 << 24
	minimalBeaconConfig.MinSlashingPenaltyQuotient = 32

	// Max operations per block
	minimalBeaconConfig.MaxProposerSlashings = 16
	minimalBeaconConfig.MaxAttesterSlashings = 2
	minimalBeaconConfig.MaxAttestations = 128
	minimalBeaconConfig.MaxDeposits = 16
	minimalBeaconConfig.MaxVoluntaryExits = 16

	// Signature domains
	minimalBeaconConfig.DomainBeaconProposer = bytesutil.ToBytes4(bytesutil.Bytes4(0))
	minimalBeaconConfig.DomainBeaconAttester = bytesutil.ToBytes4(bytesutil.Bytes4(1))
	minimalBeaconConfig.DomainRandao = bytesutil.ToBytes4(bytesutil.Bytes4(2))
	minimalBeaconConfig.DomainDeposit = bytesutil.ToBytes4(bytesutil.Bytes4(3))
	minimalBeaconConfig.DomainVoluntaryExit = bytesutil.ToBytes4(bytesutil.Bytes4(4))
	minimalBeaconConfig.GenesisForkVersion = []byte{0, 0, 0, 1}

	minimalBeaconConfig.DepositContractTreeDepth = 32
	minimalBeaconConfig.FarFutureEpoch = 1<<64 - 1

	return minimalBeaconConfig
}

// ShardMinimalSpecConfig retrieves the minimal config used in spec tests.
func ShardMinimalSpecConfig() *ShardChainConfig {
	minimalShardConfig := mainnetShardChainConfig.Copy()
	minimalShardConfig.InitialActiveShards = 4

	return minimalShardConfig
}
