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
	// Configuration related flags.
	MinimalConfig bool // MinimalConfig as defined in the spec.
	E2EConfig     bool //E2EConfig made specifically for testing, do not use except in E2E.

	// Feature related flags.
	WriteSSZStateTransitions                   bool // WriteSSZStateTransitions to tmp directory.
	InitSyncNoVerify                           bool // InitSyncNoVerify when initial syncing w/o verifying block's contents.
	DisableDynamicCommitteeSubnets             bool // Disables dynamic attestation committee subnets via p2p.
	SkipBLSVerify                              bool // Skips BLS verification across the runtime.
	EnableBackupWebhook                        bool // EnableBackupWebhook to allow database backups to trigger from monitoring port /db/backup.
	PruneEpochBoundaryStates                   bool // PruneEpochBoundaryStates prunes the epoch boundary state before last finalized check point.
	EnableSnappyDBCompression                  bool // EnableSnappyDBCompression in the database.
	ProtectProposer                            bool // ProtectProposer prevents the validator client from signing any proposals that would be considered a slashable offense.
	ProtectAttester                            bool // ProtectAttester prevents the validator client from signing any attestations that would be considered a slashable offense.
	SlasherProtection                          bool // SlasherProtection protects validator fron sending over a slashable offense over the network using external slasher.
	DisableStrictAttestationPubsubVerification bool // DisableStrictAttestationPubsubVerification will disabling strict signature verification in pubsub.
	DisableUpdateHeadPerAttestation            bool // DisableUpdateHeadPerAttestation will disabling update head on per attestation basis.
	EnableDomainDataCache                      bool // EnableDomainDataCache caches validator calls to DomainData per epoch.
	EnableStateGenSigVerify                    bool // EnableStateGenSigVerify verifies proposer and randao signatures during state gen.
	CheckHeadState                             bool // CheckHeadState checks the current headstate before retrieving the desired state from the db.
	EnableNoise                                bool // EnableNoise enables the beacon node to use NOISE instead of SECIO when performing a handshake with another peer.
	DontPruneStateStartUp                      bool // DontPruneStateStartUp disables pruning state upon beacon node start up.
	NewStateMgmt                               bool // NewStateMgmt enables the new state mgmt service.
	EnableFieldTrie                            bool // EnableFieldTrie enables the state from using field specific tries when computing the root.
	NoInitSyncBatchSaveBlocks                  bool // NoInitSyncBatchSaveBlocks disables batch save blocks mode during initial syncing.
	EnableStateRefCopy                         bool // EnableStateRefCopy copies the references to objects instead of the objects themselves when copying state fields.
	WaitForSynced                              bool // WaitForSynced uses WaitForSynced in validator startup to ensure it can communicate with the beacon node as soon as possible.
	SkipRegenHistoricalStates                  bool // SkipRegenHistoricalState skips regenerating historical states from genesis to last finalized. This enables a quick switch over to using new-state-mgmt.
	EnableInitSyncWeightedRoundRobin           bool // EnableInitSyncWeightedRoundRobin enables weighted round robin fetching optimization in initial syncing.
	ReduceAttesterStateCopy                    bool // ReduceAttesterStateCopy reduces head state copies for attester rpc.
	EnableKadDHT                               bool // EnableKadDHT stops libp2p's kademlia based discovery from running.
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

	KafkaBootstrapServers string // KafkaBootstrapServers to find kafka servers to stream blocks, attestations, etc.
	CustomGenesisDelay    uint64 // CustomGenesisDelay signals how long of a delay to set to start the chain.
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
	complainOnDeprecatedFlags(ctx)
	cfg := &Flags{}
	cfg = configureConfig(ctx, cfg)
	if ctx.Bool(devModeFlag.Name) {
		enableDevModeFlags(ctx)
	}
	delay := params.BeaconConfig().GenesisDelay
	if ctx.IsSet(customGenesisDelayFlag.Name) {
		delay = ctx.Uint64(customGenesisDelayFlag.Name)
		log.Warnf("Starting ETH2 with genesis delay of %d seconds", delay)
	}
	cfg.CustomGenesisDelay = delay
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
	if ctx.Bool(initSyncVerifyEverythingFlag.Name) {
		log.Warn("Initial syncing with verifying all block's content signatures.")
		cfg.InitSyncNoVerify = false
	} else {
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
	if ctx.Bool(enableSlasherFlag.Name) {
		log.Warn("Enable slasher connection.")
		cfg.EnableSlasherConnection = true
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
	if ctx.Bool(enableNoiseHandshake.Name) {
		log.Warn("Enabling noise handshake for peer")
		cfg.EnableNoise = true
	}
	if ctx.Bool(dontPruneStateStartUp.Name) {
		log.Warn("Not enabling state pruning upon start up")
		cfg.DontPruneStateStartUp = true
	}
	cfg.NewStateMgmt = true
	if ctx.Bool(disableNewStateMgmt.Name) {
		log.Warn("Disabling new state management service")
		cfg.NewStateMgmt = false
	}
	cfg.EnableFieldTrie = true
	if ctx.Bool(disableFieldTrie.Name) {
		log.Warn("Disabling state field trie")
		cfg.EnableFieldTrie = false
	}
	if ctx.Bool(disableInitSyncBatchSaveBlocks.Name) {
		log.Warn("Disabling init sync batch save blocks mode")
		cfg.NoInitSyncBatchSaveBlocks = true
	}
	if ctx.Bool(disableBroadcastSlashingFlag.Name) {
		log.Warn("Disabling slashing broadcasting to p2p network")
		cfg.DisableBroadcastSlashings = true
	}
	if ctx.Bool(skipRegenHistoricalStates.Name) {
		log.Warn("Enabling skipping of historical states regen")
		cfg.SkipRegenHistoricalStates = true
	}
	cfg.EnableInitSyncWeightedRoundRobin = true
	if ctx.Bool(disableInitSyncWeightedRoundRobin.Name) {
		log.Warn("Disabling weighted round robin in initial syncing")
		cfg.EnableInitSyncWeightedRoundRobin = false
	}
	cfg.EnableStateRefCopy = true
	if ctx.Bool(disableStateRefCopy.Name) {
		log.Warn("Disabling state reference copy")
		cfg.EnableStateRefCopy = false
	}
	if ctx.Bool(enableKadDht.Name) {
		log.Warn("Enabling libp2p's kademlia discovery")
		cfg.EnableKadDHT = true
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
	Init(cfg)
}

// ConfigureSlasher sets the global config based
// on what flags are enabled for the slasher client.
func ConfigureSlasher(ctx *cli.Context) {
	complainOnDeprecatedFlags(ctx)
	cfg := &Flags{}
	cfg = configureConfig(ctx, cfg)
	if ctx.Bool(enableHistoricalDetectionFlag.Name) {
		log.Warn("Enabling historical attestation detection")
		cfg.EnableHistoricalDetection = true
	}
	if ctx.Bool(disableLookbackFlag.Name) {
		log.Warn("Disabling slasher lookback")
		cfg.DisableLookback = true
	}
	Init(cfg)
}

// ConfigureValidator sets the global config based
// on what flags are enabled for the validator client.
func ConfigureValidator(ctx *cli.Context) {
	complainOnDeprecatedFlags(ctx)
	cfg := &Flags{}
	cfg = configureConfig(ctx, cfg)
	if ctx.Bool(enableProtectProposerFlag.Name) {
		log.Warn("Enabled validator proposal slashing protection.")
		cfg.ProtectProposer = true
	}
	if ctx.Bool(enableProtectAttesterFlag.Name) {
		log.Warn("Enabled validator attestation slashing protection.")
		cfg.ProtectAttester = true
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

func configureConfig(ctx *cli.Context, cfg *Flags) *Flags {
	if ctx.Bool(minimalConfigFlag.Name) {
		log.Warn("Using minimal config")
		cfg.MinimalConfig = true
		params.UseMinimalConfig()
	}
	if ctx.Bool(e2eConfigFlag.Name) {
		log.Warn("Using end-to-end testing config")
		cfg.MinimalConfig = true
		params.UseE2EConfig()
	}
	return cfg
}
