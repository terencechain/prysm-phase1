package v2

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/gofrs/flock"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/fileutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/validator/flags"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/derived"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/direct"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/remote"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
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
	configFilePath string
	walletFileLock *flock.Flock
	keymanagerKind v2keymanager.Kind
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
	walletExists, err := fileutil.HasDir(walletDir)
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
	return &Wallet{
		accountsPath:   accountsPath,
		keymanagerKind: keymanagerKind,
		walletDir:      walletDir,
	}, nil
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
	ok, err := fileutil.HasDir(walletDir)
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
	log.Infof("%s %s", au.BrightMagenta("(wallet directory)"), walletDir)
	return &Wallet{
		walletDir:      walletDir,
		accountsPath:   walletPath,
		keymanagerKind: keymanagerKind,
	}, nil
}

// SaveWallet persists the wallet's directories to disk.
func (w *Wallet) SaveWallet() error {
	if err := os.MkdirAll(w.accountsPath, DirectoryPermissions); err != nil {
		return errors.Wrap(err, "could not create wallet directory")
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

// InitializeKeymanager reads a keymanager config from disk at the wallet path,
// unmarshals it based on the wallet's keymanager kind, and returns its value.
func (w *Wallet) InitializeKeymanager(
	cliCtx *cli.Context,
	skipMnemonicConfirm bool,
) (v2keymanager.IKeymanager, error) {
	ctx := context.Background()
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
		keymanager, err = direct.NewKeymanager(cliCtx, w, cfg)
		if err != nil {
			return nil, errors.Wrap(err, "could not initialize direct keymanager")
		}
	case v2keymanager.Derived:
		cfg, err := derived.UnmarshalConfigFile(configFile)
		if err != nil {
			return nil, errors.Wrap(err, "could not unmarshal keymanager config file")
		}
		keymanager, err = derived.NewKeymanager(cliCtx, w, cfg, skipMnemonicConfirm)
		if err != nil {
			return nil, errors.Wrap(err, "could not initialize derived keymanager")
		}
	case v2keymanager.Remote:
		cfg, err := remote.UnmarshalConfigFile(configFile)
		if err != nil {
			return nil, errors.Wrap(err, "could not unmarshal keymanager config file")
		}
		keymanager, err = remote.NewKeymanager(cliCtx, 100000000, cfg)
		if err != nil {
			return nil, errors.Wrap(err, "could not initialize remote keymanager")
		}
	default:
		return nil, fmt.Errorf("keymanager kind not supported: %s", w.keymanagerKind)
	}
	return keymanager, nil
}

// Exists returns if the wallet provided exists or not.
func (w *Wallet) Exists() (bool, error) {
	accountsDir, err := os.Open(w.AccountsDir())
	if err != nil {
		return false, err
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
		return false, errors.Wrapf(err, "could not read files in directory: %s", w.AccountsDir())
	}
	// There are 2 files for derived and non-derived wallets if a wallet is properly setup.
	walletExists := len(list) >= 2
	return walletExists, nil
}

// WriteFileAtPath within the wallet directory given the desired path, filename, and raw data.
func (w *Wallet) WriteFileAtPath(ctx context.Context, filePath string, fileName string, data []byte) error {
	accountPath := filepath.Join(w.accountsPath, filePath)
	if err := os.MkdirAll(accountPath, os.ModePerm); err != nil {
		return errors.Wrapf(err, "could not create path: %s", accountPath)
	}
	fullPath := filepath.Join(accountPath, fileName)
	if err := ioutil.WriteFile(fullPath, data, params.BeaconIoConfig().ReadWritePermissions); err != nil {
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

// ReadKeymanagerConfigFromDisk opens a keymanager config file
// for reading if it exists at the wallet path.
func (w *Wallet) ReadKeymanagerConfigFromDisk(ctx context.Context) (io.ReadCloser, error) {
	configFilePath := filepath.Join(w.accountsPath, KeymanagerConfigFileName)
	if !fileutil.FileExists(configFilePath) {
		return nil, fmt.Errorf("no keymanager config file found at path: %s", w.accountsPath)
	}
	w.configFilePath = configFilePath
	return os.Open(configFilePath)

}

// LockConfigFile lock read and write to wallet file in order to prevent
// two validators from using the same keys.
func (w *Wallet) LockConfigFile(ctx context.Context) error {
	fileLock := flock.New(w.configFilePath)
	locked, err := fileLock.TryLock()
	if err != nil {
		return errors.Wrapf(err, "failed to lock wallet config file: %s", w.configFilePath)
	}
	if !locked {
		return fmt.Errorf("failed to lock wallet config file: %s", w.configFilePath)
	}
	w.walletFileLock = fileLock
	return nil
}

// UnlockWalletConfigFile unlock wallet file.
// should be called before client is closing in order to remove the file lock.
func (w *Wallet) UnlockWalletConfigFile() error {
	if w.walletFileLock == nil {
		return errors.New("trying to unlock a nil lock")
	}
	return w.walletFileLock.Unlock()
}

// WriteKeymanagerConfigToDisk takes an encoded keymanager config file
// and writes it to the wallet path.
func (w *Wallet) WriteKeymanagerConfigToDisk(ctx context.Context, encoded []byte) error {
	configFilePath := filepath.Join(w.accountsPath, KeymanagerConfigFileName)
	// Write the config file to disk.
	if err := ioutil.WriteFile(configFilePath, encoded, params.BeaconIoConfig().ReadWritePermissions); err != nil {
		return errors.Wrapf(err, "could not write %s", configFilePath)
	}
	log.WithField("configFilePath", configFilePath).Debug("Wrote keymanager config file to disk")
	return nil
}

// ReadEncryptedSeedFromDisk reads the encrypted wallet seed configuration from
// within the wallet path.
func (w *Wallet) ReadEncryptedSeedFromDisk(ctx context.Context) (io.ReadCloser, error) {
	configFilePath := filepath.Join(w.accountsPath, derived.EncryptedSeedFileName)
	if !fileutil.FileExists(configFilePath) {
		return nil, fmt.Errorf("no encrypted seed file found at path: %s", w.accountsPath)
	}
	return os.Open(configFilePath)
}

// WriteEncryptedSeedToDisk writes the encrypted wallet seed configuration
// within the wallet path.
func (w *Wallet) WriteEncryptedSeedToDisk(ctx context.Context, encoded []byte) error {
	seedFilePath := filepath.Join(w.accountsPath, derived.EncryptedSeedFileName)
	// Write the config file to disk.
	if err := ioutil.WriteFile(seedFilePath, encoded, params.BeaconIoConfig().ReadWritePermissions); err != nil {
		return errors.Wrapf(err, "could not write %s", seedFilePath)
	}
	log.WithField("seedFilePath", seedFilePath).Debug("Wrote wallet encrypted seed file to disk")
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

func openOrCreateWallet(cliCtx *cli.Context, creationFunc func(cliCtx *cli.Context) (*Wallet, error)) (*Wallet, error) {
	directory := cliCtx.String(flags.WalletDirFlag.Name)
	ok, err := fileutil.HasDir(directory)
	if err != nil {
		return nil, errors.Wrapf(err, "could not check if wallet dir %s exists", directory)
	}
	if ok {
		isEmptyWallet, err := isEmptyWallet(directory)
		if err != nil {
			return nil, errors.Wrap(err, "could not check if wallet has files")
		}
		if !isEmptyWallet {
			wallet, err := OpenWallet(cliCtx)
			if err != nil {
				return nil, errors.Wrap(err, "could not open wallet")
			}
			return wallet, nil
		}
	}
	return creationFunc(cliCtx)
}

// isEmptyWallet checks if a folder consists key directory such as `derived`, `remote` or `direct`.
// Returns true if exists, false otherwise.
func isEmptyWallet(name string) (bool, error) {
	expanded, err := fileutil.ExpandPath(name)
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
