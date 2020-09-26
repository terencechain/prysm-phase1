/*
Package featureconfig defines which features are enabled for runtime
in order to selectively enable certain features to maintain a stable runtime.

The process for implementing new features using this package is as follows:
	1. Add a new CMD flag in flags.go, and place it in the proper list(s) var for its client.
	2. Add a condition for the flag in the proper Configure function(s) below.
	3. Place any "new" behavior in the `if flagEnabled` statement.
	4. Place any "previous" behavior in the `else` statement.
	5. Ensure any tests using the new feature fail if the flag isn't enabled.
	5a. Use the following to enable your flag for tests:
	cfg := &featureconfig.Flags{
		VerifyAttestationSigs: true,
	}
	resetCfg := featureconfig.InitWithReset(cfg)
	defer resetCfg()
	6. Add the string for the flags that should be running within E2E to E2EValidatorFlags
	and E2EBeaconChainFlags.
*/
package featureconfig

import (
	"github.com/prysmaticlabs/prysm/shared/cmd"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
)

var log = logrus.WithField("prefix", "flags")

// Flags is a struct to represent which features the client will perform on runtime.
type Flags struct {
	// State locks
	NewBeaconStateLocks bool // NewStateLocks for updated beacon state locking.
	// Testnet Flags.
	AltonaTestnet  bool // AltonaTestnet defines the flag through which we can enable the node to run on the altona testnet.
	OnyxTestnet    bool // OnyxTestnet defines the flag through which we can enable the node to run on the onyx testnet.
	SpadinaTestnet bool // Spadina defines the flag through which we can enable the node to run on the spadina testnet.

	// Feature related flags.
	WriteSSZStateTransitions                   bool // WriteSSZStateTransitions to tmp directory.
	InitSyncNoVerify                           bool // InitSyncNoVerify when initial syncing w/o verifying block's contents.
	DisableDynamicCommitteeSubnets             bool // Disables dynamic attestation committee subnets via p2p.
	SkipBLSVerify                              bool // Skips BLS verification across the runtime.
	EnableBlst                                 bool // Enables new BLS library from supranational.
	EnableBackupWebhook                        bool // EnableBackupWebhook to allow database backups to trigger from monitoring port /db/backup.
	PruneEpochBoundaryStates                   bool // PruneEpochBoundaryStates prunes the epoch boundary state before last finalized check point.
	EnableSnappyDBCompression                  bool // EnableSnappyDBCompression in the database.
	LocalProtection                            bool // LocalProtection prevents the validator client from signing any messages that would be considered a slashable offense from the validators view.
	SlasherProtection                          bool // SlasherProtection protects validator fron sending over a slashable offense over the network using external slasher.
	DisableStrictAttestationPubsubVerification bool // DisableStrictAttestationPubsubVerification will disabling strict signature verification in pubsub.
	DisableUpdateHeadPerAttestation            bool // DisableUpdateHeadPerAttestation will disabling update head on per attestation basis.
	EnableDomainDataCache                      bool // EnableDomainDataCache caches validator calls to DomainData per epoch.
	EnableStateGenSigVerify                    bool // EnableStateGenSigVerify verifies proposer and randao signatures during state gen.
	CheckHeadState                             bool // CheckHeadState checks the current headstate before retrieving the desired state from the db.
	EnableNoise                                bool // EnableNoise enables the beacon node to use NOISE instead of SECIO when performing a handshake with another peer.
	DontPruneStateStartUp                      bool // DontPruneStateStartUp disables pruning state upon beacon node start up.
	WaitForSynced                              bool // WaitForSynced uses WaitForSynced in validator startup to ensure it can communicate with the beacon node as soon as possible.
	ReduceAttesterStateCopy                    bool // ReduceAttesterStateCopy reduces head state copies for attester rpc.
	Phase1                                     bool // Phase1 starts beacon node in phase 1 mode.
	EnableAccountsV2                           bool // EnableAccountsV2 for Prysm validator clients.
	BatchBlockVerify                           bool // BatchBlockVerify performs batched verification of block batches that we receive when syncing.
	InitSyncVerbose                            bool // InitSyncVerbose logs every processed block during initial syncing.
	EnableFinalizedDepositsCache               bool // EnableFinalizedDepositsCache enables utilization of cached finalized deposits.
	EnableEth1DataMajorityVote                 bool // EnableEth1DataMajorityVote uses the Voting With The Majority algorithm to vote for eth1data.
	EnableAttBroadcastDiscoveryAttempts        bool // EnableAttBroadcastDiscoveryAttempts allows the p2p service to attempt to ensure a subnet peer is present before broadcasting an attestation.
	EnablePeerScorer                           bool // EnablePeerScorer enables experimental peer scoring in p2p.

	// DisableForkChoice disables using LMD-GHOST fork choice to update
	// the head of the chain based on attestations and instead accepts any valid received block
	// as the chain head. UNSAFE, use with caution.
	DisableForkChoice bool

	// Logging related toggles.
	DisableGRPCConnectionLogs bool // Disables logging when a new grpc client has connected.

	// Slasher toggles.
	DisableBroadcastSlashings bool // DisableBroadcastSlashings disables p2p broadcasting of proposer and attester slashings.
	EnableHistoricalDetection bool // EnableHistoricalDetection disables historical attestation detection and performs detection on the chain head immediately.
	DisableLookback           bool // DisableLookback updates slasher to not use the lookback and update validator histories until epoch 0.

	// Cache toggles.
	EnableSSZCache          bool // EnableSSZCache see https://github.com/prysmaticlabs/prysm/pull/4558.
	EnableEth1DataVoteCache bool // EnableEth1DataVoteCache; see https://github.com/prysmaticlabs/prysm/issues/3106.
	EnableSlasherConnection bool // EnableSlasher enable retrieval of slashing events from a slasher instance.
	EnableBlockTreeCache    bool // EnableBlockTreeCache enable fork choice service to maintain latest filtered block tree.
	UseCheckPointInfoCache  bool // UseCheckPointInfoCache uses check point info cache to efficiently verify attestation signatures.

	KafkaBootstrapServers          string // KafkaBootstrapServers to find kafka servers to stream blocks, attestations, etc.
	AttestationAggregationStrategy string // AttestationAggregationStrategy defines aggregation strategy to be used when aggregating.
}

var featureConfig *Flags

// Get retrieves feature config.
func Get() *Flags {
	if featureConfig == nil {
		return &Flags{}
	}
	return featureConfig
}

// Init sets the global config equal to the config that is passed in.
func Init(c *Flags) {
	featureConfig = c
}

// InitWithReset sets the global config and returns function that is used to reset configuration.
func InitWithReset(c *Flags) func() {
	resetFunc := func() {
		Init(&Flags{})
	}
	Init(c)
	return resetFunc
}

// ConfigureBeaconChain sets the global config based
// on what flags are enabled for the beacon-chain client.
func ConfigureBeaconChain(ctx *cli.Context) {
	// Using Medalla as the default configuration for now.
	params.UseMedallaConfig()

	complainOnDeprecatedFlags(ctx)
	cfg := &Flags{}
	if ctx.Bool(devModeFlag.Name) {
		enableDevModeFlags(ctx)
	}
	if ctx.Bool(AltonaTestnet.Name) {
		log.Warn("Running Node on Altona Testnet")
		params.UseAltonaConfig()
		params.UseAltonaNetworkConfig()
		cfg.AltonaTestnet = true
	}
	if ctx.Bool(OnyxTestnet.Name) {
		log.Warn("Running Node on Onyx Testnet")
		params.UseOnyxConfig()
		params.UseOnyxNetworkConfig()
		cfg.OnyxTestnet = true
	}
	if ctx.Bool(SpadinaTestnet.Name) {
		log.Warn("Running Node on Spadina Testnet")
		params.UseSpadinaConfig()
		params.UseSpadinaNetworkConfig()
		cfg.SpadinaTestnet = true
	}
	if ctx.Bool(writeSSZStateTransitionsFlag.Name) {
		log.Warn("Writing SSZ states and blocks after state transitions")
		cfg.WriteSSZStateTransitions = true
	}
	if ctx.Bool(disableForkChoiceUnsafeFlag.Name) {
		log.Warn("UNSAFE: Disabled fork choice for updating chain head")
		cfg.DisableForkChoice = true
	}
	if ctx.Bool(disableDynamicCommitteeSubnets.Name) {
		log.Warn("Disabled dynamic attestation committee subnets")
		cfg.DisableDynamicCommitteeSubnets = true
	}
	cfg.EnableSSZCache = true
	if ctx.Bool(disableSSZCache.Name) {
		log.Warn("Disabled ssz cache")
		cfg.EnableSSZCache = false
	}
	cfg.InitSyncNoVerify = false
	if ctx.Bool(disableInitSyncVerifyEverythingFlag.Name) {
		log.Warn("Initial syncing while verifying only the block proposer signatures.")
		cfg.InitSyncNoVerify = true
	}
	if ctx.Bool(skipBLSVerifyFlag.Name) {
		log.Warn("UNSAFE: Skipping BLS verification at runtime")
		cfg.SkipBLSVerify = true
	}
	if ctx.Bool(enableBackupWebhookFlag.Name) {
		log.Warn("Allowing database backups to be triggered from HTTP webhook.")
		cfg.EnableBackupWebhook = true
	}
	if ctx.String(kafkaBootstrapServersFlag.Name) != "" {
		log.Warn("Enabling experimental kafka streaming.")
		cfg.KafkaBootstrapServers = ctx.String(kafkaBootstrapServersFlag.Name)
	}
	if ctx.Bool(cacheFilteredBlockTreeFlag.Name) {
		log.Warn("Enabled filtered block tree cache for fork choice.")
		cfg.EnableBlockTreeCache = true
	}
	if ctx.Bool(disableStrictAttestationPubsubVerificationFlag.Name) {
		log.Warn("Disabled strict attestation signature verification in pubsub")
		cfg.DisableStrictAttestationPubsubVerification = true
	}
	if ctx.Bool(disableUpdateHeadPerAttestation.Name) {
		log.Warn("Disabled update head on per attestation basis")
		cfg.DisableUpdateHeadPerAttestation = true
	}
	if ctx.Bool(enableStateGenSigVerify.Name) {
		log.Warn("Enabling sig verify for state gen")
		cfg.EnableStateGenSigVerify = true
	}
	if ctx.Bool(checkHeadState.Name) {
		log.Warn("Enabling check head state for chainservice")
		cfg.CheckHeadState = true
	}
	cfg.EnableNoise = true
	if ctx.Bool(disableNoiseHandshake.Name) {
		log.Warn("Disabling noise handshake for peer")
		cfg.EnableNoise = false
	}
	if ctx.Bool(dontPruneStateStartUp.Name) {
		log.Warn("Not enabling state pruning upon start up")
		cfg.DontPruneStateStartUp = true
	}
	if ctx.Bool(disableBroadcastSlashingFlag.Name) {
		log.Warn("Disabling slashing broadcasting to p2p network")
		cfg.DisableBroadcastSlashings = true
	}
	if ctx.IsSet(deprecatedP2PWhitelist.Name) {
		log.Warnf("--%s is deprecated, please use --%s", deprecatedP2PWhitelist.Name, cmd.P2PAllowList.Name)
		if err := ctx.Set(cmd.P2PAllowList.Name, ctx.String(deprecatedP2PWhitelist.Name)); err != nil {
			log.WithError(err).Error("Failed to update P2PAllowList flag")
		}
	}
	if ctx.IsSet(deprecatedP2PBlacklist.Name) {
		log.Warnf("--%s is deprecated, please use --%s", deprecatedP2PBlacklist.Name, cmd.P2PDenyList.Name)
		if err := ctx.Set(cmd.P2PDenyList.Name, ctx.String(deprecatedP2PBlacklist.Name)); err != nil {
			log.WithError(err).Error("Failed to update P2PDenyList flag")
		}
	}
	cfg.ReduceAttesterStateCopy = true
	if ctx.Bool(disableReduceAttesterStateCopy.Name) {
		log.Warn("Disabling reducing attester state copy")
		cfg.ReduceAttesterStateCopy = false
	}
	if ctx.IsSet(disableGRPCConnectionLogging.Name) {
		cfg.DisableGRPCConnectionLogs = true
	}
	if ctx.Bool(phase1.Name) {
		log.Warn("Enabling running beacon node for phase 1 configuration")
		cfg.Phase1 = true
	}
	cfg.AttestationAggregationStrategy = ctx.String(attestationAggregationStrategy.Name)
	log.Infof("Using %q strategy on attestation aggregation", cfg.AttestationAggregationStrategy)

	cfg.NewBeaconStateLocks = true
	if ctx.Bool(disableNewBeaconStateLocks.Name) {
		log.Warn("Disabling new beacon state locks")
		cfg.NewBeaconStateLocks = false
	}
	cfg.BatchBlockVerify = true
	if ctx.Bool(disableBatchBlockVerify.Name) {
		log.Warn("Disabling batch block verification when syncing.")
		cfg.BatchBlockVerify = false
	}
	if ctx.Bool(initSyncVerbose.Name) {
		log.Warn("Logging every processed block during initial syncing.")
		cfg.InitSyncVerbose = true
	}
	cfg.EnableFinalizedDepositsCache = true
	if ctx.Bool(disableFinalizedDepositsCache.Name) {
		log.Warn("Disabling finalized deposits cache")
		cfg.EnableFinalizedDepositsCache = false
	}
	if ctx.Bool(enableEth1DataMajorityVote.Name) {
		log.Warn("Enabling eth1data majority vote")
		cfg.EnableEth1DataMajorityVote = true
	}
	if ctx.Bool(enableAttBroadcastDiscoveryAttempts.Name) {
		cfg.EnableAttBroadcastDiscoveryAttempts = true
	}
	if ctx.Bool(enablePeerScorer.Name) {
		log.Warn("Enabling peer scoring in P2P")
		cfg.EnablePeerScorer = true
	}
	if ctx.Bool(checkPtInfoCache.Name) {
		log.Warn("Using advance check point info cache")
		cfg.UseCheckPointInfoCache = true
	}
	if ctx.Bool(enableBlst.Name) {
		log.Warn("Enabling new BLS library blst")
		cfg.EnableBlst = true
	}
	Init(cfg)
}

// ConfigureSlasher sets the global config based
// on what flags are enabled for the slasher client.
func ConfigureSlasher(ctx *cli.Context) {
	// Using Medalla as the default configuration for now.
	params.UseMedallaConfig()

	complainOnDeprecatedFlags(ctx)
	cfg := &Flags{}
	if ctx.Bool(disableLookbackFlag.Name) {
		log.Warn("Disabling slasher lookback")
		cfg.DisableLookback = true
	}
	Init(cfg)
}

// ConfigureValidator sets the global config based
// on what flags are enabled for the validator client.
func ConfigureValidator(ctx *cli.Context) {
	// Using Medalla as the default configuration for now.
	params.UseMedallaConfig()

	complainOnDeprecatedFlags(ctx)
	cfg := &Flags{}
	if ctx.Bool(AltonaTestnet.Name) {
		log.Warn("Running Validator on Altona Testnet")
		params.UseAltonaConfig()
		params.UseAltonaNetworkConfig()
		cfg.AltonaTestnet = true
	}
	if ctx.Bool(OnyxTestnet.Name) {
		log.Warn("Running Node on Onyx Testnet")
		params.UseOnyxConfig()
		params.UseOnyxNetworkConfig()
		cfg.OnyxTestnet = true
	}
	if ctx.Bool(SpadinaTestnet.Name) {
		log.Warn("Running Node on Spadina Testnet")
		params.UseSpadinaConfig()
		params.UseSpadinaNetworkConfig()
		cfg.SpadinaTestnet = true
	}
	if ctx.Bool(enableLocalProtectionFlag.Name) {
		cfg.LocalProtection = true
	} else {
		log.Warn("Validator slashing protection not enabled!")
	}
	cfg.EnableAccountsV2 = true
	if ctx.Bool(disableAccountsV2.Name) {
		log.Warn("Disabling v2 of Prysm validator accounts")
		log.Error(
			"Accounts v1 will be fully deprecated in Prysm within the next 2 releases! If you are still " +
				"using this functionality, please begin to upgrade by creating a v2 wallet. More information can be " +
				"found in our docs portal https://docs.prylabs.network/docs/wallet/introduction/",
		)
		cfg.EnableAccountsV2 = false
	}
	if ctx.Bool(enableExternalSlasherProtectionFlag.Name) {
		log.Warn("Enabled validator attestation and block slashing protection using an external slasher.")
		cfg.SlasherProtection = true
	}
	cfg.EnableDomainDataCache = true
	if ctx.Bool(disableDomainDataCacheFlag.Name) {
		log.Warn("Disabled domain data cache.")
		cfg.EnableDomainDataCache = false
	}
	Init(cfg)
}

// enableDevModeFlags switches development mode features on.
func enableDevModeFlags(ctx *cli.Context) {
	log.Warn("Enabling development mode flags")
	for _, f := range devModeFlags {
		log.WithField("flag", f.Names()[0]).Debug("Enabling development mode flag")
		if !ctx.IsSet(f.Names()[0]) {
			if err := ctx.Set(f.Names()[0], "true"); err != nil {
				log.WithError(err).Debug("Error enabling development mode flag")
			}
		}
	}
}

func complainOnDeprecatedFlags(ctx *cli.Context) {
	for _, f := range deprecatedFlags {
		if ctx.IsSet(f.Names()[0]) {
			log.Errorf("%s is deprecated and has no effect. Do not use this flag, it will be deleted soon.", f.Names()[0])
		}
	}
}
