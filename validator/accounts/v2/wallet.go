package v2

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"github.com/k0kubun/go-ansi"
	"github.com/logrusorgru/aurora"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/promptutil"
	"github.com/prysmaticlabs/prysm/validator/flags"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/derived"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/direct"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/remote"
	"github.com/schollz/progressbar/v3"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	keystorev4 "github.com/wealdtech/go-eth2-wallet-encryptor-keystorev4"
)

const (
	// KeymanagerConfigFileName for the keymanager used by the wallet: direct, derived, or remote.
	KeymanagerConfigFileName = "keymanageropts.json"
	// DirectoryPermissions for directories created under the wallet path.
	DirectoryPermissions = os.ModePerm
)

var (
	// ErrNoWalletFound signifies there was no wallet directory found on-disk.
	ErrNoWalletFound = errors.New(
		"no wallet found at path, please create a new wallet using `./prysm.sh validator wallet-v2 create`",
	)
	// ErrWalletExists is an error returned when a wallet already exists in the path provided.
	ErrWalletExists = errors.New("you already have a wallet at the specified path. You can " +
		"edit your wallet configuration by running ./prysm.sh validator wallet-v2 edit-config",
	)
	keymanagerKindSelections = map[v2keymanager.Kind]string{
		v2keymanager.Derived: "HD Wallet (Recommended)",
		v2keymanager.Direct:  "Non-HD Wallet (Most Basic)",
		v2keymanager.Remote:  "Remote Signing Wallet (Advanced)",
	}
)

// Wallet is a primitive in Prysm's v2 account management which
// has the capability of creating new accounts, reading existing accounts,
// and providing secure access to eth2 secrets depending on an
// associated keymanager (either direct, derived, or remote signing enabled).
type Wallet struct {
	walletDir      string
	accountsPath   string
	passwordsDir   string
	keymanagerKind v2keymanager.Kind
	walletPassword string
}

func init() {
	petname.NonDeterministicMode() // Set random account name generation.
}

// NewWallet given a set of configuration options, will leverage
// create and write a new wallet to disk for a Prysm validator.
func NewWallet(
	cliCtx *cli.Context,
	keymanagerKind v2keymanager.Kind,
) (*Wallet, error) {
	walletDir, err := inputDirectory(cliCtx, walletDirPromptText, flags.WalletDirFlag)
	// Check if the user has a wallet at the specified path.
	// If a user does not have a wallet, we instantiate one
	// based on specified options.
	walletExists, err := hasDir(walletDir)
	if err != nil {
		return nil, errors.Wrap(err, "could not check if wallet exists")
	}
	if walletExists {
		isEmptyWallet, err := isEmptyWallet(walletDir)
		if err != nil {
			return nil, errors.Wrap(err, "could not check if wallet has files")
		}
		if !isEmptyWallet {
			return nil, ErrWalletExists
		}
	}
	accountsPath := filepath.Join(walletDir, keymanagerKind.String())
	w := &Wallet{
		accountsPath:   accountsPath,
		keymanagerKind: keymanagerKind,
		walletDir:      walletDir,
	}
	if keymanagerKind == v2keymanager.Derived || keymanagerKind == v2keymanager.Direct {
		walletPassword, err := inputPassword(
			cliCtx,
			flags.WalletPasswordFileFlag,
			newWalletPasswordPromptText,
			confirmPass,
			promptutil.ValidatePasswordInput,
		)
		if err != nil {
			return nil, errors.Wrap(err, "could not get password")
		}
		w.walletPassword = walletPassword
	}
	return w, nil
}

// OpenWallet instantiates a wallet from a specified path. It checks the
// type of keymanager associated with the wallet by reading files in the wallet
// path, if applicable. If a wallet does not exist, returns an appropriate error.
func OpenWallet(cliCtx *cli.Context) (*Wallet, error) {
	// Read a wallet's directory from user input.
	walletDir, err := inputDirectory(cliCtx, walletDirPromptText, flags.WalletDirFlag)
	if err != nil {
		return nil, err
	}
	ok, err := hasDir(walletDir)
	if err != nil {
		return nil, errors.Wrap(err, "could not parse wallet directory")
	}
	if ok {
		isEmptyWallet, err := isEmptyWallet(walletDir)
		if err != nil {
			return nil, errors.Wrap(err, "could not check if wallet has files")
		}
		if isEmptyWallet {
			return nil, ErrNoWalletFound
		}
	} else {
		return nil, ErrNoWalletFound
	}
	keymanagerKind, err := readKeymanagerKindFromWalletPath(walletDir)
	if err != nil {
		return nil, errors.Wrap(err, "could not read keymanager kind for wallet")
	}
	walletPath := filepath.Join(walletDir, keymanagerKind.String())
	w := &Wallet{
		walletDir:      walletDir,
		accountsPath:   walletPath,
		keymanagerKind: keymanagerKind,
	}
	// Check if the wallet is using the new, fast keystore format.
	hasNewFormat, err := hasDir(filepath.Join(walletPath, direct.AccountsPath))
	if err != nil {
		return nil, errors.Wrap(err, "could not read wallet dir")
	}
	log.Infof("%s %s", au.BrightMagenta("(wallet directory)"), w.walletDir)
	if keymanagerKind == v2keymanager.Derived {
		validateExistingPass := func(input string) error {
			if input == "" {
				return errors.New("password input cannot be empty")
			}
			return nil
		}
		walletPassword, err := inputPassword(
			cliCtx,
			flags.WalletPasswordFileFlag,
			walletPasswordPromptText,
			noConfirmPass,
			validateExistingPass,
		)
		if err != nil {
			return nil, err
		}
		w.walletPassword = walletPassword
	}
	if keymanagerKind == v2keymanager.Direct {
		var walletPassword string
		if hasNewFormat {
			validateExistingPass := func(input string) error {
				if input == "" {
					return errors.New("password input cannot be empty")
				}
				return nil
			}
			walletPassword, err = inputPassword(
				cliCtx,
				flags.WalletPasswordFileFlag,
				walletPasswordPromptText,
				noConfirmPass,
				validateExistingPass,
			)
		} else {
			passwordsDir, err := inputDirectory(cliCtx, passwordsDirPromptText, flags.WalletPasswordsDirFlag)
			if err != nil {
				return nil, err
			}
			w.passwordsDir = passwordsDir
			au := aurora.NewAurora(true)
			log.Infof("%s %s", au.BrightMagenta("(account passwords path)"), w.passwordsDir)
			fmt.Println("\nWe have revamped how imported accounts work, improving speed significantly for your " +
				"validators as well as reducing memory and CPU requirements. This unifies all your existing accounts " +
				"into a single format protected by a strong password. You'll need to set a new password for this " +
				"updated wallet format")
			walletPassword, err = inputPassword(
				cliCtx,
				flags.WalletPasswordFileFlag,
				newWalletPasswordPromptText,
				confirmPass,
				promptutil.ValidatePasswordInput,
			)
		}
		if err != nil {
			return nil, err
		}
		w.walletPassword = walletPassword
	}
	return w, nil
}

// SaveWallet persists the wallet's directories to disk.
func (w *Wallet) SaveWallet() error {
	if err := os.MkdirAll(w.accountsPath, DirectoryPermissions); err != nil {
		return errors.Wrap(err, "could not create wallet directory")
	}
	if w.keymanagerKind == v2keymanager.Direct && w.passwordsDir != "" {
		if err := os.MkdirAll(w.passwordsDir, DirectoryPermissions); err != nil {
			return errors.Wrap(err, "could not create passwords directory")
		}
	}
	return nil
}

// KeymanagerKind used by the wallet.
func (w *Wallet) KeymanagerKind() v2keymanager.Kind {
	return w.keymanagerKind
}

// AccountsDir for the wallet.
func (w *Wallet) AccountsDir() string {
	return w.accountsPath
}

// Password for the wallet.
func (w *Wallet) Password() string {
	return w.walletPassword
}

// InitializeKeymanager reads a keymanager config from disk at the wallet path,
// unmarshals it based on the wallet's keymanager kind, and returns its value.
func (w *Wallet) InitializeKeymanager(
	ctx context.Context,
	skipMnemonicConfirm bool,
) (v2keymanager.IKeymanager, error) {
	configFile, err := w.ReadKeymanagerConfigFromDisk(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "could not read keymanager config")
	}
	var keymanager v2keymanager.IKeymanager
	switch w.KeymanagerKind() {
	case v2keymanager.Direct:
		cfg, err := direct.UnmarshalConfigFile(configFile)
		if err != nil {
			return nil, errors.Wrap(err, "could not unmarshal keymanager config file")
		}
		keymanager, err = direct.NewKeymanager(ctx, w, cfg)
		if err != nil {
			return nil, errors.Wrap(err, "could not initialize direct keymanager")
		}
	case v2keymanager.Derived:
		cfg, err := derived.UnmarshalConfigFile(configFile)
		if err != nil {
			return nil, errors.Wrap(err, "could not unmarshal keymanager config file")
		}
		keymanager, err = derived.NewKeymanager(ctx, w, cfg, skipMnemonicConfirm, w.walletPassword)
		if err != nil {
			return nil, errors.Wrap(err, "could not initialize derived keymanager")
		}
	case v2keymanager.Remote:
		cfg, err := remote.UnmarshalConfigFile(configFile)
		if err != nil {
			return nil, errors.Wrap(err, "could not unmarshal keymanager config file")
		}
		keymanager, err = remote.NewKeymanager(ctx, 100000000, cfg)
		if err != nil {
			return nil, errors.Wrap(err, "could not initialize remote keymanager")
		}
	default:
		return nil, fmt.Errorf("keymanager kind not supported: %s", w.keymanagerKind)
	}
	return keymanager, nil
}

// ListDirs in wallet accounts path.
func (w *Wallet) ListDirs() ([]string, error) {
	accountsDir, err := os.Open(w.AccountsDir())
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := accountsDir.Close(); err != nil {
			log.WithField(
				"directory", w.AccountsDir(),
			).Errorf("Could not close accounts directory: %v", err)
		}
	}()

	list, err := accountsDir.Readdirnames(0) // 0 to read all files and folders.
	if err != nil {
		return nil, errors.Wrapf(err, "could not read files in directory: %s", w.AccountsDir())
	}
	dirNames := make([]string, 0)
	for _, item := range list {
		ok, err := hasDir(filepath.Join(w.AccountsDir(), item))
		if err != nil {
			return nil, errors.Wrapf(err, "could not parse directory: %v", err)
		}
		if ok {
			dirNames = append(dirNames, item)
		}
	}
	return dirNames, nil
}

// WriteFileAtPath within the wallet directory given the desired path, filename, and raw data.
func (w *Wallet) WriteFileAtPath(ctx context.Context, filePath string, fileName string, data []byte) error {
	accountPath := filepath.Join(w.accountsPath, filePath)
	if err := os.MkdirAll(accountPath, os.ModePerm); err != nil {
		return errors.Wrapf(err, "could not create path: %s", accountPath)
	}
	fullPath := filepath.Join(accountPath, fileName)
	if err := ioutil.WriteFile(fullPath, data, os.ModePerm); err != nil {
		return errors.Wrapf(err, "could not write %s", filePath)
	}
	log.WithFields(logrus.Fields{
		"path":     fullPath,
		"fileName": fileName,
	}).Debug("Wrote new file at path")
	return nil
}

// ReadFileAtPath within the wallet directory given the desired path and filename.
func (w *Wallet) ReadFileAtPath(ctx context.Context, filePath string, fileName string) ([]byte, error) {
	accountPath := filepath.Join(w.accountsPath, filePath)
	if err := os.MkdirAll(accountPath, os.ModePerm); err != nil {
		return nil, errors.Wrapf(err, "could not create path: %s", accountPath)
	}
	fullPath := filepath.Join(accountPath, fileName)
	matches, err := filepath.Glob(fullPath)
	if err != nil {
		return []byte{}, errors.Wrap(err, "could not find file")
	}
	if len(matches) == 0 {
		return []byte{}, fmt.Errorf("no files found %s", fullPath)
	}
	rawData, err := ioutil.ReadFile(matches[0])
	if err != nil {
		return nil, errors.Wrapf(err, "could not read %s", filePath)
	}
	return rawData, nil
}

// FileNameAtPath return the full file name for the requested file. It allows for finding the file
// with a regex pattern.
func (w *Wallet) FileNameAtPath(ctx context.Context, filePath string, fileName string) (string, error) {
	accountPath := filepath.Join(w.accountsPath, filePath)
	if err := os.MkdirAll(accountPath, os.ModePerm); err != nil {
		return "", errors.Wrapf(err, "could not create path: %s", accountPath)
	}
	fullPath := filepath.Join(accountPath, fileName)
	matches, err := filepath.Glob(fullPath)
	if err != nil {
		return "", errors.Wrap(err, "could not find file")
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("no files found %s", fullPath)
	}
	fullFileName := filepath.Base(matches[0])
	return fullFileName, nil
}

// AccountTimestamp retrieves the timestamp from a given keystore file name.
func AccountTimestamp(fileName string) (time.Time, error) {
	timestampStart := strings.LastIndex(fileName, "-") + 1
	timestampEnd := strings.LastIndex(fileName, ".")
	// Return an error if the text we expect cannot be found.
	if timestampStart == -1 || timestampEnd == -1 {
		return time.Unix(0, 0), fmt.Errorf("could not find timestamp in file name %s", fileName)
	}
	unixTimestampStr, err := strconv.ParseInt(fileName[timestampStart:timestampEnd], 10, 64)
	if err != nil {
		return time.Unix(0, 0), errors.Wrapf(err, "could not parse account created at timestamp: %s", fileName)
	}
	unixTimestamp := time.Unix(unixTimestampStr, 0)
	return unixTimestamp, nil
}

// ReadKeymanagerConfigFromDisk opens a keymanager config file
// for reading if it exists at the wallet path.
func (w *Wallet) ReadKeymanagerConfigFromDisk(ctx context.Context) (io.ReadCloser, error) {
	configFilePath := filepath.Join(w.accountsPath, KeymanagerConfigFileName)
	if !fileExists(configFilePath) {
		return nil, fmt.Errorf("no keymanager config file found at path: %s", w.accountsPath)
	}
	return os.Open(configFilePath)
}

// WriteKeymanagerConfigToDisk takes an encoded keymanager config file
// and writes it to the wallet path.
func (w *Wallet) WriteKeymanagerConfigToDisk(ctx context.Context, encoded []byte) error {
	configFilePath := filepath.Join(w.accountsPath, KeymanagerConfigFileName)
	// Write the config file to disk.
	if err := ioutil.WriteFile(configFilePath, encoded, os.ModePerm); err != nil {
		return errors.Wrapf(err, "could not write %s", configFilePath)
	}
	log.WithField("configFilePath", configFilePath).Debug("Wrote keymanager config file to disk")
	return nil
}

// ReadEncryptedSeedFromDisk reads the encrypted wallet seed configuration from
// within the wallet path.
func (w *Wallet) ReadEncryptedSeedFromDisk(ctx context.Context) (io.ReadCloser, error) {
	configFilePath := filepath.Join(w.accountsPath, derived.EncryptedSeedFileName)
	if !fileExists(configFilePath) {
		return nil, fmt.Errorf("no encrypted seed file found at path: %s", w.accountsPath)
	}
	return os.Open(configFilePath)
}

// WriteEncryptedSeedToDisk writes the encrypted wallet seed configuration
// within the wallet path.
func (w *Wallet) WriteEncryptedSeedToDisk(ctx context.Context, encoded []byte) error {
	seedFilePath := filepath.Join(w.accountsPath, derived.EncryptedSeedFileName)
	// Write the config file to disk.
	if err := ioutil.WriteFile(seedFilePath, encoded, os.ModePerm); err != nil {
		return errors.Wrapf(err, "could not write %s", seedFilePath)
	}
	log.WithField("seedFilePath", seedFilePath).Debug("Wrote wallet encrypted seed file to disk")
	return nil
}

// ReadPasswordFromDisk --
func (w *Wallet) ReadPasswordFromDisk(ctx context.Context, passwordFileName string) (string, error) {
	fullPath := filepath.Join(w.passwordsDir, passwordFileName)
	rawData, err := ioutil.ReadFile(fullPath)
	if err != nil {
		return "", errors.Wrapf(err, "could not read %s", fullPath)
	}
	return string(rawData), nil
}

// enterPasswordForAccount checks if a user has a password specified for the new account
// either from a file or from stdin. Then, it saves the password to the wallet.
func (w *Wallet) enterPasswordForAccount(cliCtx *cli.Context, accountName string, pubKey []byte) error {
	au := aurora.NewAurora(true)
	var password string
	var err error
	if cliCtx.IsSet(flags.AccountPasswordFileFlag.Name) {
		passwordFilePath := cliCtx.String(flags.AccountPasswordFileFlag.Name)
		data, err := ioutil.ReadFile(passwordFilePath)
		if err != nil {
			return err
		}
		password = string(data)
		err = w.checkPasswordForAccount(accountName, password)
		if err != nil && strings.Contains(err.Error(), "invalid checksum") {
			return fmt.Errorf("invalid password entered for account with public key %#x", pubKey)
		}
		if err != nil {
			return err
		}
	} else {
		pubKeyStr := fmt.Sprintf("%#x", bytesutil.Trunc(pubKey))
		attemptingPassword := true
		// Loop asking for the password until the user enters it correctly.
		for attemptingPassword {
			// Ask the user for the password to their account.
			password, err = inputWeakPassword(
				cliCtx,
				flags.AccountPasswordFileFlag,
				fmt.Sprintf(passwordForAccountPromptText, au.BrightGreen(pubKeyStr)),
			)
			if err != nil {
				return errors.Wrap(err, "could not input password")
			}
			err = w.checkPasswordForAccount(accountName, password)
			if err != nil && strings.Contains(err.Error(), "invalid checksum") {
				fmt.Print(au.Red("X").Bold())
				fmt.Print(au.Red("\nIncorrect password entered, please try again"))
				continue
			}
			if err != nil {
				return err
			}
			attemptingPassword = false
			fmt.Print(au.Green("✔️\n").Bold())
		}
	}
	ctx := context.Background()
	if err := w.WritePasswordToDisk(ctx, accountName+direct.PasswordFileSuffix, password); err != nil {
		return errors.Wrap(err, "could not write password to disk")
	}
	return nil
}

func (w *Wallet) enterPasswordForAllAccounts(cliCtx *cli.Context, accountNames []string, pubKeys [][]byte) error {
	au := aurora.NewAurora(true)
	var password string
	var err error
	ctx := context.Background()
	if cliCtx.IsSet(flags.AccountPasswordFileFlag.Name) {
		passwordFilePath := cliCtx.String(flags.AccountPasswordFileFlag.Name)
		data, err := ioutil.ReadFile(passwordFilePath)
		if err != nil {
			return err
		}
		password = string(data)
		for i := 0; i < len(accountNames); i++ {
			err = w.checkPasswordForAccount(accountNames[i], password)
			if err != nil && strings.Contains(err.Error(), "invalid checksum") {
				return fmt.Errorf("invalid password for account with public key %#x", pubKeys[i])
			}
			if err != nil {
				return err
			}
			if err := w.WritePasswordToDisk(ctx, accountNames[i]+direct.PasswordFileSuffix, password); err != nil {
				return errors.Wrap(err, "could not write password to disk")
			}
		}
	} else {
		password, err = inputWeakPassword(
			cliCtx,
			flags.AccountPasswordFileFlag,
			"Enter the password for your imported accounts",
		)
		fmt.Println("Importing accounts, this may take a while...")
		bar := progressbar.NewOptions(
			len(accountNames),
			progressbar.OptionFullWidth(),
			progressbar.OptionSetWriter(ansi.NewAnsiStdout()),
			progressbar.OptionEnableColorCodes(true),
			progressbar.OptionSetTheme(progressbar.Theme{
				Saucer:        "[green]=[reset]",
				SaucerHead:    "[green]>[reset]",
				SaucerPadding: " ",
				BarStart:      "[",
				BarEnd:        "]",
			}),
			progressbar.OptionOnCompletion(func() { fmt.Println() }),
			progressbar.OptionSetDescription("Importing accounts"),
		)
		ctx := context.Background()
		for i := 0; i < len(accountNames); i++ {
			// We check if the individual account unlocks with the global password.
			err = w.checkPasswordForAccount(accountNames[i], password)
			if err != nil && strings.Contains(err.Error(), "invalid checksum") {
				// If the password fails for an individual account, we ask the user to input
				// that individual account's password until it succeeds.
				individualPassword, err := w.askUntilPasswordConfirms(cliCtx, accountNames[i], pubKeys[i])
				if err != nil {
					return err
				}
				if err := w.WritePasswordToDisk(ctx, accountNames[i]+direct.PasswordFileSuffix, individualPassword); err != nil {
					return errors.Wrap(err, "could not write password to disk")
				}
				if err := bar.Add(1); err != nil {
					return errors.Wrap(err, "could not add to progress bar")
				}
				continue
			}
			if err != nil {
				return err
			}
			fmt.Printf("Finished importing %#x\n", au.BrightMagenta(bytesutil.Trunc(pubKeys[i])))
			if err := w.WritePasswordToDisk(ctx, accountNames[i]+direct.PasswordFileSuffix, password); err != nil {
				return errors.Wrap(err, "could not write password to disk")
			}
			if err := bar.Add(1); err != nil {
				return errors.Wrap(err, "could not add to progress bar")
			}
		}
	}
	return nil
}

func (w *Wallet) askUntilPasswordConfirms(cliCtx *cli.Context, accountName string, pubKey []byte) (string, error) {
	// Loop asking for the password until the user enters it correctly.
	var password string
	var err error
	for {
		password, err = inputWeakPassword(
			cliCtx,
			flags.AccountPasswordFileFlag,
			fmt.Sprintf(passwordForAccountPromptText, bytesutil.Trunc(pubKey)),
		)
		if err != nil {
			return "", errors.Wrap(err, "could not input password")
		}
		err = w.checkPasswordForAccount(accountName, password)
		if err != nil && strings.Contains(err.Error(), "invalid checksum") {
			fmt.Println(au.Red("Incorrect password entered, please try again"))
			continue
		}
		if err != nil {
			return "", err
		}
		break
	}
	return password, nil
}

func (w *Wallet) checkPasswordForAccount(accountName string, password string) error {
	encoded, err := w.ReadFileAtPath(context.Background(), accountName, direct.KeystoreFileName)
	if err != nil {
		return errors.Wrap(err, "could not read keystore file")
	}
	keystoreJSON := &v2keymanager.Keystore{}
	if err := json.Unmarshal(encoded, &keystoreJSON); err != nil {
		return errors.Wrap(err, "could not decode json")
	}
	decryptor := keystorev4.New()
	_, err = decryptor.Decrypt(keystoreJSON.Crypto, password)
	if err != nil {
		return errors.Wrap(err, "could not decrypt keystore")
	}
	return nil
}

// WritePasswordToDisk --
func (w *Wallet) WritePasswordToDisk(ctx context.Context, passwordFileName string, password string) error {
	passwordPath := filepath.Join(w.passwordsDir, passwordFileName)
	if err := ioutil.WriteFile(passwordPath, []byte(password), os.ModePerm); err != nil {
		return errors.Wrapf(err, "could not write %s", passwordPath)
	}
	return nil
}

func readKeymanagerKindFromWalletPath(walletPath string) (v2keymanager.Kind, error) {
	walletItem, err := os.Open(walletPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err := walletItem.Close(); err != nil {
			log.WithField(
				"path", walletPath,
			).Errorf("Could not close wallet directory: %v", err)
		}
	}()
	list, err := walletItem.Readdirnames(0) // 0 to read all files and folders.
	if err != nil {
		return 0, fmt.Errorf("could not read files in directory: %s", walletPath)
	}
	for _, n := range list {
		keymanagerKind, err := v2keymanager.ParseKind(n)
		if err == nil {
			return keymanagerKind, nil
		}
	}
	return 0, errors.New("no keymanager folder, 'direct', 'remote', nor 'derived' found in wallet path")
}

func createOrOpenWallet(cliCtx *cli.Context, creationFunc func(cliCtx *cli.Context) (*Wallet, error)) (*Wallet, error) {
	directory := cliCtx.String(flags.WalletDirFlag.Name)
	ok, err := hasDir(directory)
	if err != nil {
		return nil, errors.Wrapf(err, "could not check if wallet dir %s exists", directory)
	}
	if ok {
		isEmptyWallet, err := isEmptyWallet(directory)
		if err != nil {
			return nil, errors.Wrap(err, "could not check if wallet has files")
		}
		if !isEmptyWallet {
			return OpenWallet(cliCtx)
		}
	}
	return creationFunc(cliCtx)
}

// Returns true if a file is not a directory and exists
// at the specified path.
func fileExists(filename string) bool {
	filePath, err := expandPath(filename)
	if err != nil {
		return false
	}
	info, err := os.Stat(filePath)
	if os.IsNotExist(err) {
		return false
	}
	return !info.IsDir()
}

// Checks if a directory indeed exists at the specified path.
func hasDir(dirPath string) (bool, error) {
	fullPath, err := expandPath(dirPath)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(fullPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if info == nil {
		return false, err
	}
	return info.IsDir(), err
}

// isEmptyWallet checks if a folder consists key directory such as `derived`, `remote` or `direct`.
// Returns true if exists, false otherwise.
func isEmptyWallet(name string) (bool, error) {
	expanded, err := expandPath(name)
	if err != nil {
		return false, err
	}
	f, err := os.Open(expanded)
	if err != nil {
		return false, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Debugf("Could not close directory: %s", expanded)
		}
	}()
	names, err := f.Readdirnames(-1)
	if err == io.EOF {
		return true, nil
	}

	for _, n := range names {
		// Nil error means input name is `derived`, `remote` or `direct`, the wallet is not empty.
		_, err := v2keymanager.ParseKind(n)
		if err == nil {
			return false, nil
		}
	}

	return true, err
}
