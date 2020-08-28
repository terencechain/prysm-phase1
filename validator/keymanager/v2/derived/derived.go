package derived

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	validatorpb "github.com/prysmaticlabs/prysm/proto/validator/accounts/v2"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/depositutil"
	"github.com/prysmaticlabs/prysm/shared/fileutil"
	"github.com/prysmaticlabs/prysm/shared/petnames"
	"github.com/prysmaticlabs/prysm/shared/promptutil"
	"github.com/prysmaticlabs/prysm/shared/rand"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/iface"
	"github.com/prysmaticlabs/prysm/validator/flags"
	"github.com/sirupsen/logrus"
	"github.com/tyler-smith/go-bip39"
	"github.com/urfave/cli/v2"
	util "github.com/wealdtech/go-eth2-util"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"
)

var log = logrus.WithField("prefix", "derived-keymanager-v2")

const (
	// EIPVersion used by this derived keymanager implementation.
	EIPVersion = "EIP-2334"
	// WithdrawalKeyDerivationPathTemplate defining the hierarchical path for withdrawal
	// keys for Prysm eth2 validators. According to EIP-2334, the format is as follows:
	// m / purpose / coin_type / account_index / withdrawal_key
	WithdrawalKeyDerivationPathTemplate = "m/12381/3600/%d/0"
	// ValidatingKeyDerivationPathTemplate defining the hierarchical path for validating
	// keys for Prysm eth2 validators. According to EIP-2334, the format is as follows:
	// m / purpose / coin_type / account_index / withdrawal_key / validating_key
	ValidatingKeyDerivationPathTemplate = "m/12381/3600/%d/0/0"
	// EncryptedSeedFileName for persisting a wallet's seed when using a derived keymanager.
	EncryptedSeedFileName       = "seed.encrypted.json"
	newWalletPasswordPromptText = "New wallet password"
	walletPasswordPromptText    = "Wallet password"
	confirmPasswordPromptText   = "Confirm password"
)

type passwordConfirm int

const (
	// An enum to indicate to the prompt that confirming the password is not needed.
	noConfirmPass passwordConfirm = iota
	// An enum to indicate to the prompt to confirm the password entered.
	confirmPass
)

// Config for a derived keymanager.
type Config struct {
	DerivedPathStructure string
	DerivedEIPNumber     string
}

// Keymanager implementation for derived, HD keymanager using EIP-2333 and EIP-2334.
type Keymanager struct {
	wallet            iface.Wallet
	cfg               *Config
	mnemonicGenerator SeedPhraseFactory
	keysCache         map[[48]byte]bls.SecretKey
	lock              sync.RWMutex
	seedCfg           *SeedConfig
	seed              []byte
	accountsPassword  string
}

// SeedConfig json file representation as a Go struct.
type SeedConfig struct {
	Crypto      map[string]interface{} `json:"crypto"`
	ID          string                 `json:"uuid"`
	NextAccount uint64                 `json:"next_account"`
	Version     uint                   `json:"version"`
	Name        string                 `json:"name"`
}

// DefaultConfig for a derived keymanager implementation.
func DefaultConfig() *Config {
	return &Config{
		DerivedPathStructure: "m / purpose / coin_type / account_index / withdrawal_key / validating_key",
		DerivedEIPNumber:     EIPVersion,
	}
}

// NewKeymanager instantiates a new derived keymanager from configuration options.
func NewKeymanager(
	cliCtx *cli.Context,
	wallet iface.Wallet,
	cfg *Config,
	skipMnemonicConfirm bool,
) (*Keymanager, error) {
	walletExists, err := wallet.Exists()
	if err != nil {
		return nil, err
	}
	var accountsPassword string
	// If the user does not have any accounts in their wallet, we ask them to
	// set a new wallet password, which will be used for encrypting/decrypting
	// their wallet secret to and from disk.
	if !walletExists {
		accountsPassword, err = inputPassword(
			cliCtx,
			flags.WalletPasswordFileFlag,
			newWalletPasswordPromptText,
			confirmPass,
			promptutil.ValidatePasswordInput,
		)
	} else {
		validateExistingPass := func(input string) error {
			if input == "" {
				return errors.New("password input cannot be empty")
			}
			return nil
		}
		accountsPassword, err = inputPassword(
			cliCtx,
			flags.WalletPasswordFileFlag,
			walletPasswordPromptText,
			noConfirmPass,
			validateExistingPass,
		)
	}

	// Check if the wallet seed file exists. If it does not, we initialize one
	// by creating a new mnemonic and writing the encrypted file to disk.
	ctx := context.Background()
	var encodedSeedFile []byte
	if !fileutil.FileExists(filepath.Join(wallet.AccountsDir(), EncryptedSeedFileName)) {
		seedConfig, err := initializeWalletSeedFile(accountsPassword, skipMnemonicConfirm)
		if err != nil {
			return nil, errors.Wrap(err, "could not initialize new wallet seed file")
		}
		encodedSeedFile, err = marshalEncryptedSeedFile(seedConfig)
		if err != nil {
			return nil, errors.Wrap(err, "could not marshal encrypted wallet seed file")
		}
		if err = wallet.WriteEncryptedSeedToDisk(ctx, encodedSeedFile); err != nil {
			return nil, errors.Wrap(err, "could not write encrypted wallet seed config to disk")
		}
	} else {
		seedConfigFile, err := wallet.ReadEncryptedSeedFromDisk(ctx)
		if err != nil {
			return nil, errors.Wrap(err, "could not read encrypted seed file from disk")
		}
		encodedSeedFile, err = ioutil.ReadAll(seedConfigFile)
		if err != nil {
			return nil, errors.Wrap(err, "could not read seed configuration file contents")
		}
		defer func() {
			if err := seedConfigFile.Close(); err != nil {
				log.Errorf("Could not close keymanager config file: %v", err)
			}
		}()
	}
	seedConfig := &SeedConfig{}
	if err := json.Unmarshal(encodedSeedFile, seedConfig); err != nil {
		return nil, errors.Wrap(err, "could not unmarshal seed configuration")
	}
	decryptor := keystorev4.New()
	seed, err := decryptor.Decrypt(seedConfig.Crypto, accountsPassword)
	if err != nil {
		return nil, errors.Wrap(err, "could not decrypt seed configuration with password")
	}
	k := &Keymanager{
		wallet: wallet,
		cfg:    cfg,
		mnemonicGenerator: &EnglishMnemonicGenerator{
			skipMnemonicConfirm: skipMnemonicConfirm,
		},
		seedCfg:          seedConfig,
		seed:             seed,
		accountsPassword: accountsPassword,
		keysCache:        make(map[[48]byte]bls.SecretKey),
	}
	// We initialize a cache of public key -> secret keys
	// used to retrieve secrets keys for the accounts via the unlocked wallet.
	// This cache is needed to process Sign requests using a validating public key.
	if err := k.initializeSecretKeysCache(); err != nil {
		return nil, errors.Wrap(err, "could not initialize secret keys cache")
	}
	return k, nil
}

// KeymanagerForPhrase instantiates a new derived keymanager from configuration and an existing mnemonic phrase provided.
func KeymanagerForPhrase(
	cliCtx *cli.Context,
	wallet iface.Wallet,
	cfg *Config,
	mnemonic string,
) (*Keymanager, error) {
	walletExists, err := wallet.Exists()
	if err != nil {
		return nil, err
	}
	var accountsPassword string
	// If the user does not have any accounts in their wallet, we ask them to
	// set a new wallet password, which will be used for encrypting/decrypting
	// their wallet secret to and from disk.
	if !walletExists {
		accountsPassword, err = inputPassword(
			cliCtx,
			flags.WalletPasswordFileFlag,
			newWalletPasswordPromptText,
			confirmPass,
			promptutil.ValidatePasswordInput,
		)
	} else {
		validateExistingPass := func(input string) error {
			if input == "" {
				return errors.New("password input cannot be empty")
			}
			return nil
		}
		accountsPassword, err = inputPassword(
			cliCtx,
			flags.WalletPasswordFileFlag,
			walletPasswordPromptText,
			noConfirmPass,
			validateExistingPass,
		)
	}

	// Check if the wallet seed file exists. If it does not, we initialize one
	// by creating a new mnemonic and writing the encrypted file to disk.
	ctx := context.Background()
	var encodedSeedFile []byte
	seedConfig, err := seedFileFromMnemonic(mnemonic, accountsPassword)
	if err != nil {
		return nil, errors.Wrap(err, "could not initialize new wallet seed file")
	}
	encodedSeedFile, err = marshalEncryptedSeedFile(seedConfig)
	if err != nil {
		return nil, errors.Wrap(err, "could not marshal encrypted wallet seed file")
	}
	if err = wallet.WriteEncryptedSeedToDisk(ctx, encodedSeedFile); err != nil {
		return nil, errors.Wrap(err, "could not write encrypted wallet seed config to disk")
	}
	decryptor := keystorev4.New()
	seed, err := decryptor.Decrypt(seedConfig.Crypto, accountsPassword)
	if err != nil {
		return nil, errors.Wrap(err, "could not decrypt seed configuration with password")
	}
	k := &Keymanager{
		wallet: wallet,
		cfg:    cfg,
		mnemonicGenerator: &EnglishMnemonicGenerator{
			skipMnemonicConfirm: true,
		},
		seedCfg:          seedConfig,
		seed:             seed,
		accountsPassword: accountsPassword,
		keysCache:        make(map[[48]byte]bls.SecretKey),
	}
	// We initialize a cache of public key -> secret keys
	// used to retrieve secrets keys for the accounts via the unlocked wallet.
	// This cache is needed to process Sign requests using a validating public key.
	if err := k.initializeSecretKeysCache(); err != nil {
		return nil, errors.Wrap(err, "could not initialize secret keys cache")
	}
	return k, nil
}

// UnmarshalConfigFile attempts to JSON unmarshal a derived keymanager
// configuration file into the *Config{} struct.
func UnmarshalConfigFile(r io.ReadCloser) (*Config, error) {
	enc, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := r.Close(); err != nil {
			log.Errorf("Could not close keymanager config file: %v", err)
		}
	}()
	cfg := &Config{}
	if err := json.Unmarshal(enc, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// MarshalConfigFile returns a marshaled configuration file for a keymanager.
func MarshalConfigFile(ctx context.Context, cfg *Config) ([]byte, error) {
	return json.MarshalIndent(cfg, "", "\t")
}

// Config returns the derived keymanager configuration.
func (dr *Keymanager) Config() *Config {
	return dr.cfg
}

// NextAccountNumber managed by the derived keymanager.
func (dr *Keymanager) NextAccountNumber(ctx context.Context) uint64 {
	return dr.seedCfg.NextAccount
}

// WriteEncryptedSeedToWallet given a mnemonic phrase, is able to regenerate a wallet seed
// encrypt it, and write it to the wallet's path.
func (dr *Keymanager) WriteEncryptedSeedToWallet(ctx context.Context, mnemonic string) error {
	seedConfig, err := seedFileFromMnemonic(mnemonic, dr.accountsPassword)
	if err != nil {
		return errors.Wrap(err, "could not initialize new wallet seed file")
	}
	seedConfigFile, err := marshalEncryptedSeedFile(seedConfig)
	if err != nil {
		return errors.Wrap(err, "could not marshal encrypted wallet seed file")
	}
	if err := dr.wallet.WriteEncryptedSeedToDisk(ctx, seedConfigFile); err != nil {
		return errors.Wrap(err, "could not write encrypted wallet seed config to disk")
	}
	return nil
}

// ValidatingAccountNames for the derived keymanager.
func (dr *Keymanager) ValidatingAccountNames(ctx context.Context) ([]string, error) {
	names := make([]string, 0)
	for i := uint64(0); i < dr.seedCfg.NextAccount; i++ {
		validatingKeyPath := fmt.Sprintf(ValidatingKeyDerivationPathTemplate, i)
		validatingKey, err := util.PrivateKeyFromSeedAndPath(dr.seed, validatingKeyPath)
		if err != nil {
			return nil, errors.Wrap(err, "could not derive validating key")
		}
		names = append(names, petnames.DeterministicName(validatingKey.PublicKey().Marshal(), "-"))
	}
	return names, nil
}

// CreateAccount for a derived keymanager implementation. This utilizes
// the EIP-2335 keystore standard for BLS12-381 keystores. It uses the EIP-2333 and EIP-2334
// for hierarchical derivation of BLS secret keys and a common derivation path structure for
// persisting accounts to disk. Each account stores the generated keystore.json file.
// The entire derived wallet seed phrase can be recovered from a BIP-39 english mnemonic.
func (dr *Keymanager) CreateAccount(ctx context.Context, logAccountInfo bool) (string, error) {
	withdrawalKeyPath := fmt.Sprintf(WithdrawalKeyDerivationPathTemplate, dr.seedCfg.NextAccount)
	validatingKeyPath := fmt.Sprintf(ValidatingKeyDerivationPathTemplate, dr.seedCfg.NextAccount)
	withdrawalKey, err := util.PrivateKeyFromSeedAndPath(dr.seed, withdrawalKeyPath)
	if err != nil {
		return "", errors.Wrapf(err, "failed to create withdrawal key for account %d", dr.seedCfg.NextAccount)
	}
	validatingKey, err := util.PrivateKeyFromSeedAndPath(dr.seed, validatingKeyPath)
	if err != nil {
		return "", errors.Wrapf(err, "failed to create validating key for account %d", dr.seedCfg.NextAccount)
	}

	// Upon confirmation of the withdrawal key, proceed to display
	// and write associated deposit data to disk.
	blsValidatingKey, err := bls.SecretKeyFromBytes(validatingKey.Marshal())
	if err != nil {
		return "", err
	}
	blsWithdrawalKey, err := bls.SecretKeyFromBytes(withdrawalKey.Marshal())
	if err != nil {
		return "", err
	}
	// Upon confirmation of the withdrawal key, proceed to display
	// and write associated deposit data to disk.
	tx, _, err := depositutil.GenerateDepositTransaction(blsValidatingKey, blsWithdrawalKey)
	if err != nil {
		return "", errors.Wrap(err, "could not generate deposit transaction data")
	}

	// Log the deposit transaction data to the user.
	fmt.Printf(`
======================Eth1 Deposit Transaction Data================
%#x
===================================================================`, tx.Data())
	fmt.Println("")

	// Finally, write the account creation timestamps as a files.
	newAccountNumber := dr.seedCfg.NextAccount
	if logAccountInfo {
		log.WithFields(logrus.Fields{
			"accountNumber":       newAccountNumber,
			"withdrawalPublicKey": fmt.Sprintf("%#x", withdrawalKey.PublicKey().Marshal()),
			"validatingPublicKey": fmt.Sprintf("%#x", validatingKey.PublicKey().Marshal()),
			"withdrawalKeyPath":   path.Join(dr.wallet.AccountsDir(), withdrawalKeyPath),
			"validatingKeyPath":   path.Join(dr.wallet.AccountsDir(), validatingKeyPath),
		}).Info("Successfully created new validator account")
	}
	dr.seedCfg.NextAccount++
	encodedCfg, err := marshalEncryptedSeedFile(dr.seedCfg)
	if err != nil {
		return "", errors.Wrap(err, "could not marshal encrypted seed file")
	}
	if err := dr.wallet.WriteEncryptedSeedToDisk(ctx, encodedCfg); err != nil {
		return "", errors.Wrap(err, "could not write encrypted seed file to disk")
	}
	return fmt.Sprintf("%d", newAccountNumber), nil
}

// Sign signs a message using a validator key.
func (dr *Keymanager) Sign(ctx context.Context, req *validatorpb.SignRequest) (bls.Signature, error) {
	rawPubKey := req.PublicKey
	if rawPubKey == nil {
		return nil, errors.New("nil public key in request")
	}
	dr.lock.RLock()
	defer dr.lock.RUnlock()
	secretKey, ok := dr.keysCache[bytesutil.ToBytes48(rawPubKey)]
	if !ok {
		return nil, errors.New("no signing key found in keys cache")
	}
	return secretKey.Sign(req.SigningRoot), nil
}

// FetchValidatingPublicKeys fetches the list of validating public keys from the keymanager.
func (dr *Keymanager) FetchValidatingPublicKeys(ctx context.Context) ([][48]byte, error) {
	// Return the public keys from the cache if they match the
	// number of accounts from the wallet.
	publicKeys := make([][48]byte, dr.seedCfg.NextAccount)
	dr.lock.RLock()
	defer dr.lock.RUnlock()
	if dr.keysCache != nil && uint64(len(dr.keysCache)) == dr.seedCfg.NextAccount {
		var i int
		for k := range dr.keysCache {
			publicKeys[i] = k
			i++
		}
		return publicKeys, nil
	}
	for i := uint64(0); i < dr.seedCfg.NextAccount; i++ {
		validatingKeyPath := fmt.Sprintf(ValidatingKeyDerivationPathTemplate, i)
		validatingKey, err := util.PrivateKeyFromSeedAndPath(dr.seed, validatingKeyPath)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create validating key for account %d", i)
		}
		publicKeys = append(publicKeys, bytesutil.ToBytes48(validatingKey.PublicKey().Marshal()))
	}
	return publicKeys, nil
}

// FetchWithdrawalPublicKeys fetches the list of withdrawal public keys from keymanager
func (dr *Keymanager) FetchWithdrawalPublicKeys(ctx context.Context) ([][48]byte, error) {
	publicKeys := make([][48]byte, 0)
	for i := uint64(0); i < dr.seedCfg.NextAccount; i++ {
		withdrawalKeyPath := fmt.Sprintf(WithdrawalKeyDerivationPathTemplate, i)
		withdrawalKey, err := util.PrivateKeyFromSeedAndPath(dr.seed, withdrawalKeyPath)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create validating key for account %d", i)
		}
		publicKeys = append(publicKeys, bytesutil.ToBytes48(withdrawalKey.PublicKey().Marshal()))
	}
	return publicKeys, nil
}

// DepositDataForAccount with a given index returns the RLP encoded eth1 deposit transaction data.
func (dr *Keymanager) DepositDataForAccount(accountIndex uint64) ([]byte, error) {
	withdrawalKeyPath := fmt.Sprintf(WithdrawalKeyDerivationPathTemplate, accountIndex)
	validatingKeyPath := fmt.Sprintf(ValidatingKeyDerivationPathTemplate, accountIndex)
	withdrawalKey, err := util.PrivateKeyFromSeedAndPath(dr.seed, withdrawalKeyPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create withdrawal key for account %d", accountIndex)
	}
	validatingKey, err := util.PrivateKeyFromSeedAndPath(dr.seed, validatingKeyPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create validating key for account %d", accountIndex)
	}

	// Upon confirmation of the withdrawal key, proceed to display
	// and write associated deposit data to disk.
	blsValidatingKey, err := bls.SecretKeyFromBytes(validatingKey.Marshal())
	if err != nil {
		return nil, err
	}
	blsWithdrawalKey, err := bls.SecretKeyFromBytes(withdrawalKey.Marshal())
	if err != nil {
		return nil, err
	}
	tx, _, err := depositutil.GenerateDepositTransaction(blsValidatingKey, blsWithdrawalKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not generate deposit transaction data")
	}
	return tx.Data(), nil
}

func (dr *Keymanager) initializeSecretKeysCache() error {
	dr.lock.Lock()
	defer dr.lock.Unlock()
	for i := uint64(0); i < dr.seedCfg.NextAccount; i++ {
		validatingKeyPath := fmt.Sprintf(ValidatingKeyDerivationPathTemplate, i)
		derivedKey, err := util.PrivateKeyFromSeedAndPath(dr.seed, validatingKeyPath)
		if err != nil {
			return errors.Wrapf(err, "failed to derive validating key for account %s", validatingKeyPath)
		}
		validatorSigningKey, err := bls.SecretKeyFromBytes(derivedKey.Marshal())
		if err != nil {
			return errors.Wrapf(
				err,
				"could not instantiate bls secret key from bytes for account: %s",
				validatingKeyPath,
			)
		}

		// Update a simple cache of public key -> secret key utilized
		// for fast signing access in the keymanager.
		dr.keysCache[bytesutil.ToBytes48(validatorSigningKey.PublicKey().Marshal())] = validatorSigningKey
	}
	return nil
}

func inputPassword(
	cliCtx *cli.Context,
	passwordFileFlag *cli.StringFlag,
	promptText string,
	confirmPassword passwordConfirm,
	passwordValidator func(input string) error,
) (string, error) {
	if cliCtx.IsSet(passwordFileFlag.Name) {
		passwordFilePathInput := cliCtx.String(passwordFileFlag.Name)
		data, err := fileutil.ReadFileAsBytes(passwordFilePathInput)
		if err != nil {
			return "", errors.Wrap(err, "could not read file as bytes")
		}
		enteredPassword := strings.TrimRight(string(data), "\r\n")
		if err := passwordValidator(enteredPassword); err != nil {
			return "", errors.Wrap(err, "password did not pass validation")
		}
		return enteredPassword, nil
	}
	var hasValidPassword bool
	var walletPassword string
	var err error
	for !hasValidPassword {
		walletPassword, err = promptutil.PasswordPrompt(promptText, passwordValidator)
		if err != nil {
			return "", fmt.Errorf("could not read account password: %v", err)
		}

		if confirmPassword == confirmPass {
			passwordConfirmation, err := promptutil.PasswordPrompt(confirmPasswordPromptText, passwordValidator)
			if err != nil {
				return "", fmt.Errorf("could not read password confirmation: %v", err)
			}
			if walletPassword != passwordConfirmation {
				log.Error("Passwords do not match")
				continue
			}
			hasValidPassword = true
		} else {
			return walletPassword, nil
		}
	}
	return walletPassword, nil
}

// Creates a new, encrypted seed using a password input
// and persists its encrypted file metadata to disk under the wallet path.
func initializeWalletSeedFile(password string, skipMnemonicConfirm bool) (*SeedConfig, error) {
	mnemonicRandomness := make([]byte, 32)
	if _, err := rand.NewGenerator().Read(mnemonicRandomness); err != nil {
		return nil, errors.Wrap(err, "could not initialize mnemonic source of randomness")
	}
	m := &EnglishMnemonicGenerator{
		skipMnemonicConfirm: skipMnemonicConfirm,
	}
	phrase, err := m.Generate(mnemonicRandomness)
	if err != nil {
		return nil, errors.Wrap(err, "could not generate wallet seed")
	}
	if err := m.ConfirmAcknowledgement(phrase); err != nil {
		return nil, errors.Wrap(err, "could not confirm mnemonic acknowledgement")
	}
	walletSeed := bip39.NewSeed(phrase, "")
	encryptor := keystorev4.New()
	cryptoFields, err := encryptor.Encrypt(walletSeed, password)
	if err != nil {
		return nil, errors.Wrap(err, "could not encrypt seed phrase into keystore")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.Wrap(err, "could not generate unique UUID")
	}
	return &SeedConfig{
		Crypto:      cryptoFields,
		ID:          id.String(),
		NextAccount: 0,
		Version:     encryptor.Version(),
		Name:        encryptor.Name(),
	}, nil
}

// Uses the provided mnemonic seed phrase to generate the
// appropriate seed file for recovering a derived wallets.
func seedFileFromMnemonic(mnemonic string, password string) (*SeedConfig, error) {
	if ok := bip39.IsMnemonicValid(mnemonic); !ok {
		return nil, bip39.ErrInvalidMnemonic
	}
	walletSeed := bip39.NewSeed(mnemonic, "")
	encryptor := keystorev4.New()
	cryptoFields, err := encryptor.Encrypt(walletSeed, password)
	if err != nil {
		return nil, errors.Wrap(err, "could not encrypt seed phrase into keystore")
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.Wrap(err, "could not generate unique UUID")
	}
	return &SeedConfig{
		Crypto:      cryptoFields,
		ID:          id.String(),
		NextAccount: 0,
		Version:     encryptor.Version(),
		Name:        encryptor.Name(),
	}, nil
}

// marshalEncryptedSeedFile json encodes the seed configuration for a derived keymanager.
func marshalEncryptedSeedFile(seedCfg *SeedConfig) ([]byte, error) {
	return json.MarshalIndent(seedCfg, "", "\t")
}
