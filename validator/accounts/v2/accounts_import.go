package v2

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/fileutil"
	"github.com/prysmaticlabs/prysm/shared/promptutil"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/prompt"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/wallet"
	"github.com/prysmaticlabs/prysm/validator/flags"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/direct"
	"github.com/urfave/cli/v2"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"
)

var derivationPathRegex = regexp.MustCompile("m_12381_3600_([0-9]+)_([0-9]+)_([0-9]+)")

// byDerivationPath implements sort.Interface based on a
// derivation path present in a keystore filename, if any. This
// will allow us to sort filenames such as keystore-m_12381_3600_1_0_0.json
// in a directory and import them nicely in order of the derivation path.
type byDerivationPath []string

func (fileNames byDerivationPath) Len() int { return len(fileNames) }
func (fileNames byDerivationPath) Less(i, j int) bool {
	// We check if file name at index i has a derivation path
	// in the filename. If it does not, then it is not less than j, and
	// we should swap it towards the end of the sorted list.
	if !derivationPathRegex.MatchString(fileNames[i]) {
		return false
	}
	derivationPathA := derivationPathRegex.FindString(fileNames[i])
	derivationPathB := derivationPathRegex.FindString(fileNames[j])
	if derivationPathA == "" {
		return false
	}
	if derivationPathB == "" {
		return true
	}
	a, err := strconv.Atoi(accountIndexFromFileName(derivationPathA))
	if err != nil {
		return false
	}
	b, err := strconv.Atoi(accountIndexFromFileName(derivationPathB))
	if err != nil {
		return false
	}
	return a < b
}

func (fileNames byDerivationPath) Swap(i, j int) {
	fileNames[i], fileNames[j] = fileNames[j], fileNames[i]
}

// ImportAccountsConfig defines values to run the import accounts function.
type ImportAccountsConfig struct {
	Wallet          *wallet.Wallet
	Keystores       []*v2keymanager.Keystore
	AccountPassword string
}

// ImportAccountsCli can import external, EIP-2335 compliant keystore.json files as
// new accounts into the Prysm validator wallet. This uses the CLI to extract
// values necessary to run the function.
func ImportAccountsCli(cliCtx *cli.Context) error {
	w, err := wallet.OpenWalletOrElseCli(cliCtx, func(cliCtx *cli.Context) (*wallet.Wallet, error) {
		cfg, err := extractWalletCreationConfigFromCli(cliCtx, v2keymanager.Direct)
		if err != nil {
			return nil, err
		}
		w := wallet.New(&wallet.Config{
			KeymanagerKind: cfg.WalletCfg.KeymanagerKind,
			WalletDir:      cfg.WalletCfg.WalletDir,
			WalletPassword: cfg.WalletCfg.WalletPassword,
		})
		if err = createDirectKeymanagerWallet(cliCtx.Context, w); err != nil {
			return nil, errors.Wrap(err, "could not create keymanager")
		}
		// We store the hashed password to disk.
		if err := w.SaveHashedPassword(cliCtx.Context); err != nil {
			return nil, err
		}
		log.WithField("wallet-path", cfg.WalletCfg.WalletDir).Info(
			"Successfully created new wallet",
		)
		return w, nil
	})
	if err != nil {
		return errors.Wrap(err, "could not initialize wallet")
	}

	// Check if the user wishes to import a one-off, private key directly
	// as an account into the Prysm validator.
	if cliCtx.IsSet(flags.ImportPrivateKeyFileFlag.Name) {
		return importPrivateKeyAsAccount(cliCtx, w)
	}

	keysDir, err := prompt.InputDirectory(cliCtx, prompt.ImportKeysDirPromptText, flags.KeysDirFlag)
	if err != nil {
		return errors.Wrap(err, "could not parse keys directory")
	}
	// Consider that the keysDir might be a path to a specific file and handle accordingly.
	isDir, err := fileutil.HasDir(keysDir)
	if err != nil {
		return errors.Wrap(err, "could not determine if path is a directory")
	}
	keystoresImported := make([]*v2keymanager.Keystore, 0)
	if isDir {
		files, err := ioutil.ReadDir(keysDir)
		if err != nil {
			return errors.Wrap(err, "could not read dir")
		}
		if len(files) == 0 {
			return fmt.Errorf("directory %s has no files, cannot import from it", keysDir)
		}
		filesInDir := make([]string, 0)
		for i := 0; i < len(files); i++ {
			if files[i].IsDir() {
				continue
			}
			filesInDir = append(filesInDir, files[i].Name())
		}
		// Sort the imported keystores by derivation path if they
		// specify this value in their filename.
		sort.Sort(byDerivationPath(filesInDir))
		for _, name := range filesInDir {
			keystore, err := readKeystoreFile(cliCtx.Context, filepath.Join(keysDir, name))
			if err != nil && strings.Contains(err.Error(), "could not decode keystore json") {
				continue
			} else if err != nil {
				return errors.Wrapf(err, "could not import keystore at path: %s", name)
			}
			keystoresImported = append(keystoresImported, keystore)
		}
	} else {
		keystore, err := readKeystoreFile(cliCtx.Context, keysDir)
		if err != nil {
			return errors.Wrap(err, "could not import keystore")
		}
		keystoresImported = append(keystoresImported, keystore)
	}

	var accountsPassword string
	if cliCtx.IsSet(flags.AccountPasswordFileFlag.Name) {
		passwordFilePath := cliCtx.String(flags.AccountPasswordFileFlag.Name)
		data, err := ioutil.ReadFile(passwordFilePath)
		if err != nil {
			return err
		}
		accountsPassword = string(data)
	} else {
		accountsPassword, err = promptutil.PasswordPrompt(
			"Enter the password for your imported accounts", promptutil.NotEmpty,
		)
		if err != nil {
			return fmt.Errorf("could not read account password: %v", err)
		}
	}
	fmt.Println("Importing accounts, this may take a while...")
	if err := ImportAccounts(cliCtx.Context, &ImportAccountsConfig{
		Wallet:          w,
		Keystores:       keystoresImported,
		AccountPassword: accountsPassword,
	}); err != nil {
		return err
	}
	fmt.Printf(
		"Successfully imported %s accounts, view all of them by running accounts-v2 list\n",
		au.BrightMagenta(strconv.Itoa(len(keystoresImported))),
	)
	return nil
}

// ImportAccounts can import external, EIP-2335 compliant keystore.json files as
// new accounts into the Prysm validator wallet.
func ImportAccounts(ctx context.Context, cfg *ImportAccountsConfig) error {
	if cfg.Wallet.KeymanagerKind() != v2keymanager.Direct {
		return errors.New(
			"only non-HD wallets can import accounts, try creating a new wallet with wallet-v2 create",
		)
	}
	opts, err := cfg.Wallet.ReadKeymanagerConfigFromDisk(ctx)
	if err != nil {
		return err
	}
	directOpts, err := direct.UnmarshalOptionsFile(opts)
	if err != nil {
		return err
	}
	km, err := direct.NewKeymanager(ctx, &direct.SetupConfig{
		Wallet: cfg.Wallet,
		Opts:   directOpts,
	})
	if err != nil {
		return err
	}
	return km.ImportKeystores(
		ctx,
		cfg.Keystores,
		cfg.AccountPassword,
	)
}

// Imports a one-off file containing a private key as a hex string into
// the Prysm validator's accounts.
func importPrivateKeyAsAccount(cliCtx *cli.Context, wallet *wallet.Wallet) error {
	privKeyFile := cliCtx.String(flags.ImportPrivateKeyFileFlag.Name)
	fullPath, err := fileutil.ExpandPath(privKeyFile)
	if err != nil {
		return errors.Wrapf(err, "could not expand file path for %s", privKeyFile)
	}
	if !fileutil.FileExists(fullPath) {
		return fmt.Errorf("file %s does not exist", fullPath)
	}
	privKeyHex, err := ioutil.ReadFile(fullPath)
	if err != nil {
		return errors.Wrapf(err, "could not read private key file at path %s", fullPath)
	}
	privKeyString := string(privKeyHex)
	if len(privKeyString) > 2 && strings.Contains(privKeyString, "0x") {
		privKeyString = privKeyString[2:] // Strip the 0x prefix, if any.
	}
	privKeyBytes, err := hex.DecodeString(strings.TrimRight(privKeyString, "\r\n"))
	if err != nil {
		return errors.Wrap(
			err, "could not decode file as hex string, does the file contain a valid hex string?",
		)
	}
	privKey, err := bls.SecretKeyFromBytes(privKeyBytes)
	if err != nil {
		return errors.Wrap(err, "not a valid BLS private key")
	}
	keystore, err := createKeystoreFromPrivateKey(privKey, wallet.Password())
	if err != nil {
		return errors.Wrap(err, "could not encrypt private key into a keystore file")
	}
	if err := ImportAccounts(
		cliCtx.Context,
		&ImportAccountsConfig{
			Wallet:          wallet,
			AccountPassword: wallet.Password(),
			Keystores:       []*v2keymanager.Keystore{keystore},
		},
	); err != nil {
		return errors.Wrap(err, "could not import keystore into wallet")
	}
	fmt.Printf(
		"Imported account with public key %#x, view all accounts by running accounts-v2 list\n",
		au.BrightMagenta(bytesutil.Trunc(privKey.PublicKey().Marshal())),
	)
	return nil
}

func readKeystoreFile(ctx context.Context, keystoreFilePath string) (*v2keymanager.Keystore, error) {
	keystoreBytes, err := ioutil.ReadFile(keystoreFilePath)
	if err != nil {
		return nil, errors.Wrap(err, "could not read keystore file")
	}
	keystoreFile := &v2keymanager.Keystore{}
	if err := json.Unmarshal(keystoreBytes, keystoreFile); err != nil {
		return nil, errors.Wrap(err, "could not decode keystore json")
	}
	if keystoreFile.Pubkey == "" {
		return nil, errors.New("could not decode keystore json")
	}
	return keystoreFile, nil
}

func createKeystoreFromPrivateKey(privKey bls.SecretKey, walletPassword string) (*v2keymanager.Keystore, error) {
	encryptor := keystorev4.New()
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, err
	}
	cryptoFields, err := encryptor.Encrypt(privKey.Marshal(), walletPassword)
	if err != nil {
		return nil, errors.Wrapf(
			err,
			"could not encrypt private key with public key %#x",
			privKey.PublicKey().Marshal(),
		)
	}
	return &v2keymanager.Keystore{
		Crypto:  cryptoFields,
		ID:      id.String(),
		Version: encryptor.Version(),
		Pubkey:  fmt.Sprintf("%x", privKey.PublicKey().Marshal()),
		Name:    encryptor.Name(),
	}, nil
}

// Extracts the account index, j, from a derivation path in a file name
// with the format m_12381_3600_j_0_0.
func accountIndexFromFileName(derivationPath string) string {
	derivationPath = derivationPath[13:]
	accIndexEnd := strings.Index(derivationPath, "_")
	return derivationPath[:accIndexEnd]
}
