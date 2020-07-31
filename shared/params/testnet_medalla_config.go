package params

// UseMedallaNetworkConfig uses the Medalla specific
// network config.
func UseMedallaNetworkConfig() {
	cfg := BeaconNetworkConfig().Copy()
	cfg.ContractDeploymentBlock = 3085928
	cfg.DepositContractAddress = "0x07b39F4fDE4A38bACe212b546dAc87C58DfE3fDC"
	cfg.ChainID = 5   // Chain ID of eth1 goerli testnet.
	cfg.NetworkID = 5 // Network ID of eth1 goerli testnet.
	cfg.BootstrapNodes = []string{}

	OverrideBeaconNetworkConfig(cfg)
}

// MedallaConfig defines the config for the
// medalla testnet.
func MedallaConfig() *BeaconChainConfig {
	cfg := MainnetConfig().Copy()
	cfg.MinGenesisTime = 1596546000
	cfg.GenesisForkVersion = []byte{0x00, 0x00, 0x00, 0x01}
	return cfg
}

// UseMedallaConfig sets the main beacon chain
// config for medalla.
func UseMedallaConfig() {
	beaconConfig = MedallaConfig()
}
