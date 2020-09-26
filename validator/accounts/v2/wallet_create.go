package v2

import (
	"context"
	"fmt"

	"github.com/manifoldco/promptui"
	"github.com/pkg/errors"
	"github.com/prysmaticlabs/prysm/shared/fileutil"
	"github.com/prysmaticlabs/prysm/shared/promptutil"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/prompt"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/wallet"
	"github.com/prysmaticlabs/prysm/validator/flags"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/derived"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/direct"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/remote"
	"github.com/urfave/cli/v2"
)

// CreateWalletConfig defines the parameters needed to call the create wallet functions.
type CreateWalletConfig struct {
	WalletCfg            *wallet.Config
	RemoteKeymanagerOpts *remote.KeymanagerOpts
	SkipMnemonicConfirm  bool
}

// CreateAndSaveWalletCli from user input with a desired keymanager. If a
// wallet already exists in the path, it suggests the user alternatives
// such as how to edit their existing wallet configuration.
func CreateAndSaveWalletCli(cliCtx *cli.Context) (*wallet.Wallet, error) {
	keymanagerKind, err := extractKeymanagerKindFromCli(cliCtx)
	if err != nil {
		return nil, err
	}
	createWalletConfig, err := extractWalletCreationConfigFromCli(cliCtx, keymanagerKind)
	if err != nil {
		return nil, err
	}

	dir := createWalletConfig.WalletCfg.WalletDir
	dirExists, err := fileutil.HasDir(dir)
	if err != nil {
		return nil, err
	}
	if dirExists {
		return nil, errors.New("a wallet already exists at this location. Please input an" +
			" alternative location for the new wallet or remove the current wallet")
	}
	w, err := CreateWalletWithKeymanager(cliCtx.Context, createWalletConfig)
	if err != nil {
		return nil, errors.Wrap(err, "could not create wallet with keymanager")
	}
	// We store the hashed password to disk.
	if err := w.SaveHashedPassword(cliCtx.Context); err != nil {
		return nil, errors.Wrap(err, "could not save hashed password to database")
	}
	return w, nil
}

// CreateWalletWithKeymanager specified by configuration options.
func CreateWalletWithKeymanager(ctx context.Context, cfg *CreateWalletConfig) (*wallet.Wallet, error) {
	if err := wallet.Exists(cfg.WalletCfg.WalletDir); err != nil {
		if !errors.Is(err, wallet.ErrNoWalletFound) {
			return nil, errors.Wrap(err, "could not check if wallet exists")
		}
	}
	w := wallet.New(&wallet.Config{
		WalletDir:      cfg.WalletCfg.WalletDir,
		KeymanagerKind: cfg.WalletCfg.KeymanagerKind,
		WalletPassword: cfg.WalletCfg.WalletPassword,
	})
	var err error
	switch w.KeymanagerKind() {
	case v2keymanager.Direct:
		if err = createDirectKeymanagerWallet(ctx, w); err != nil {
			return nil, errors.Wrap(err, "could not initialize wallet with direct keymanager")
		}
		log.WithField("--wallet-dir", cfg.WalletCfg.WalletDir).Info(
			"Successfully created wallet with on-disk keymanager configuration. " +
				"Make a new validator account with ./prysm.sh validator accounts-v2 create",
		)
	case v2keymanager.Derived:
		if err = createDerivedKeymanagerWallet(ctx, w, cfg.SkipMnemonicConfirm); err != nil {
			return nil, errors.Wrap(err, "could not initialize wallet with derived keymanager")
		}
		log.WithField("--wallet-dir", cfg.WalletCfg.WalletDir).Info(
			"Successfully created HD wallet and saved configuration to disk. " +
				"Make a new validator account with ./prysm.sh validator accounts-2 create",
		)
	case v2keymanager.Remote:
		if err = createRemoteKeymanagerWallet(ctx, w, cfg.RemoteKeymanagerOpts); err != nil {
			return nil, errors.Wrap(err, "could not initialize wallet with remote keymanager")
		}
		log.WithField("--wallet-dir", cfg.WalletCfg.WalletDir).Info(
			"Successfully created wallet with remote keymanager configuration",
		)
	default:
		return nil, errors.Wrapf(err, "keymanager type %s is not supported", w.KeymanagerKind())
	}
	return w, nil
}

func extractKeymanagerKindFromCli(cliCtx *cli.Context) (v2keymanager.Kind, error) {
	return inputKeymanagerKind(cliCtx)
}

func extractWalletCreationConfigFromCli(cliCtx *cli.Context, keymanagerKind v2keymanager.Kind) (*CreateWalletConfig, error) {
	walletDir, err := prompt.InputDirectory(cliCtx, prompt.WalletDirPromptText, flags.WalletDirFlag)
	if err != nil {
		return nil, err
	}
	walletPassword, err := promptutil.InputPassword(
		cliCtx,
		flags.WalletPasswordFileFlag,
		wallet.NewWalletPasswordPromptText,
		wallet.ConfirmPasswordPromptText,
		true, /* Should confirm password */
		promptutil.ValidatePasswordInput,
	)
	if err != nil {
		return nil, err
	}
	createWalletConfig := &CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      walletDir,
			KeymanagerKind: keymanagerKind,
			WalletPassword: walletPassword,
		},
		SkipMnemonicConfirm: cliCtx.Bool(flags.SkipDepositConfirmationFlag.Name),
	}

	if keymanagerKind == v2keymanager.Remote {
		opts, err := prompt.InputRemoteKeymanagerConfig(cliCtx)
		if err != nil {
			return nil, errors.Wrap(err, "could not input remote keymanager config")
		}
		createWalletConfig.RemoteKeymanagerOpts = opts
	}
	return createWalletConfig, nil
}

func createDirectKeymanagerWallet(ctx context.Context, wallet *wallet.Wallet) error {
	if wallet == nil {
		return errors.New("nil wallet")
	}
	if err := wallet.SaveWallet(); err != nil {
		return errors.Wrap(err, "could not save wallet to disk")
	}
	defaultOpts := direct.DefaultKeymanagerOpts()
	keymanagerConfig, err := direct.MarshalOptionsFile(ctx, defaultOpts)
	if err != nil {
		return errors.Wrap(err, "could not marshal keymanager config file")
	}
	if err := wallet.WriteKeymanagerConfigToDisk(ctx, keymanagerConfig); err != nil {
		return errors.Wrap(err, "could not write keymanager config to disk")
	}
	return nil
}

func createDerivedKeymanagerWallet(ctx context.Context, wallet *wallet.Wallet, skipMnemonicConfirm bool) error {
	keymanagerConfig, err := derived.MarshalOptionsFile(ctx, derived.DefaultKeymanagerOpts())
	if err != nil {
		return errors.Wrap(err, "could not marshal keymanager config file")
	}
	if err := wallet.SaveWallet(); err != nil {
		return errors.Wrap(err, "could not save wallet to disk")
	}
	if err := wallet.WriteKeymanagerConfigToDisk(ctx, keymanagerConfig); err != nil {
		return errors.Wrap(err, "could not write keymanager config to disk")
	}
	_, err = wallet.InitializeKeymanager(ctx, skipMnemonicConfirm)
	if err != nil {
		return errors.Wrap(err, "could not initialize keymanager")
	}
	return nil
}

func createRemoteKeymanagerWallet(ctx context.Context, wallet *wallet.Wallet, opts *remote.KeymanagerOpts) error {
	keymanagerConfig, err := remote.MarshalOptionsFile(ctx, opts)
	if err != nil {
		return errors.Wrap(err, "could not marshal config file")
	}
	if err := wallet.SaveWallet(); err != nil {
		return errors.Wrap(err, "could not save wallet to disk")
	}
	if err := wallet.WriteKeymanagerConfigToDisk(ctx, keymanagerConfig); err != nil {
		return errors.Wrap(err, "could not write keymanager config to disk")
	}
	return nil
}

func inputKeymanagerKind(cliCtx *cli.Context) (v2keymanager.Kind, error) {
	if cliCtx.IsSet(flags.KeymanagerKindFlag.Name) {
		return v2keymanager.ParseKind(cliCtx.String(flags.KeymanagerKindFlag.Name))
	}
	promptSelect := promptui.Select{
		Label: "Select a type of wallet",
		Items: []string{
			wallet.KeymanagerKindSelections[v2keymanager.Derived],
			wallet.KeymanagerKindSelections[v2keymanager.Direct],
			wallet.KeymanagerKindSelections[v2keymanager.Remote],
		},
	}
	selection, _, err := promptSelect.Run()
	if err != nil {
		return v2keymanager.Direct, fmt.Errorf("could not select wallet type: %v", prompt.FormatPromptError(err))
	}
	return v2keymanager.Kind(selection), nil
}
