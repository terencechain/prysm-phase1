package v2

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/cmd"
	"github.com/prysmaticlabs/prysm/shared/promptutil"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/prompt"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/wallet"
	"github.com/prysmaticlabs/prysm/validator/client"
	"github.com/prysmaticlabs/prysm/validator/flags"
	v2 "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/urfave/cli/v2"
	"google.golang.org/grpc"
)

type performExitCfg struct {
	validatorClient  ethpb.BeaconNodeValidatorClient
	nodeClient       ethpb.NodeClient
	keymanager       v2.IKeymanager
	rawPubKeys       [][]byte
	formattedPubKeys []string
}

const exitPassphrase = "Exit my validator"

// ExitAccountsCli performs a voluntary exit on one or more accounts.
func ExitAccountsCli(cliCtx *cli.Context, r io.Reader) error {
	validatingPublicKeys, keymanager, err := prepareWallet(cliCtx)
	if err != nil {
		return err
	}

	rawPubKeys, formattedPubKeys, err := interact(cliCtx, r, validatingPublicKeys)
	if err != nil {
		return err
	}
	// User decided to cancel the voluntary exit.
	if rawPubKeys == nil && formattedPubKeys == nil {
		return nil
	}

	validatorClient, nodeClient, err := prepareClients(cliCtx)
	if err != nil {
		return err
	}
	cfg := performExitCfg{
		*validatorClient,
		*nodeClient,
		keymanager,
		rawPubKeys,
		formattedPubKeys,
	}

	formattedExitedKeys, err := performExit(cliCtx, cfg)
	if err != nil {
		return err
	}

	if len(formattedExitedKeys) > 0 {
		log.WithField("publicKeys", strings.Join(formattedExitedKeys, ", ")).
			Info("Voluntary exit was successful for the accounts listed")
	} else {
		log.Info("No successful voluntary exits")
	}

	return nil
}

func prepareWallet(cliCtx *cli.Context) ([][48]byte, v2.IKeymanager, error) {
	w, err := wallet.OpenWalletOrElseCli(cliCtx, func(cliCtx *cli.Context) (*wallet.Wallet, error) {
		return nil, errors.New(
			"no wallet found, no accounts to exit",
		)
	})
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not open wallet")
	}

	keymanager, err := w.InitializeKeymanager(cliCtx.Context, false /* skip mnemonic confirm */)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not initialize keymanager")
	}
	validatingPublicKeys, err := keymanager.FetchValidatingPublicKeys(cliCtx.Context)
	if err != nil {
		return nil, nil, err
	}
	if len(validatingPublicKeys) == 0 {
		return nil, nil, errors.New("wallet is empty, no accounts to perform voluntary exit")
	}

	return validatingPublicKeys, keymanager, nil
}

func interact(cliCtx *cli.Context, r io.Reader, validatingPublicKeys [][48]byte) ([][]byte, []string, error) {
	// Allow the user to interactively select the accounts to exit or optionally
	// provide them via cli flags as a string of comma-separated, hex strings.
	filteredPubKeys, err := filterPublicKeysFromUserInput(
		cliCtx,
		flags.VoluntaryExitPublicKeysFlag,
		validatingPublicKeys,
		prompt.SelectAccountsVoluntaryExitPromptText,
	)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not filter public keys for voluntary exit")
	}
	rawPubKeys := make([][]byte, len(filteredPubKeys))
	formattedPubKeys := make([]string, len(filteredPubKeys))
	for i, pk := range filteredPubKeys {
		pubKeyBytes := pk.Marshal()
		rawPubKeys[i] = pubKeyBytes
		formattedPubKeys[i] = fmt.Sprintf("%#x", bytesutil.Trunc(pubKeyBytes))
	}
	allAccountStr := strings.Join(formattedPubKeys, ", ")
	if !cliCtx.IsSet(flags.VoluntaryExitPublicKeysFlag.Name) {
		if len(filteredPubKeys) == 1 {
			promptText := "Are you sure you want to perform a voluntary exit on 1 account? (%s) Y/N"
			resp, err := promptutil.ValidatePrompt(
				r, fmt.Sprintf(promptText, au.BrightGreen(formattedPubKeys[0])), promptutil.ValidateYesOrNo,
			)
			if err != nil {
				return nil, nil, err
			}
			if strings.ToLower(resp) == "n" {
				return nil, nil, nil
			}
		} else {
			promptText := "Are you sure you want to perform a voluntary exit on %d accounts? (%s) Y/N"
			if len(filteredPubKeys) == len(validatingPublicKeys) {
				promptText = fmt.Sprintf(
					"Are you sure you want to perform a voluntary exit on all accounts? Y/N (%s)",
					au.BrightGreen(allAccountStr))
			} else {
				promptText = fmt.Sprintf(promptText, len(filteredPubKeys), au.BrightGreen(allAccountStr))
			}
			resp, err := promptutil.ValidatePrompt(r, promptText, promptutil.ValidateYesOrNo)
			if err != nil {
				return nil, nil, err
			}
			if strings.ToLower(resp) == "n" {
				return nil, nil, nil
			}
		}
	}

	promptHeader := au.Red("===============IMPORTANT===============")
	promptDescription := "Withdrawing funds is not possible in Phase 0 of the system. " +
		"Please navigate to the following website and make sure you understand the current implications " +
		"of a voluntary exit before making the final decision:"
	promptURL := au.Blue("https://docs.prylabs.network/docs/wallet/nondeterministic/exiting-a-validator/#withdrawal-delay-warning")
	promptQuestion := "If you still want to continue with the voluntary exit, please input the passphrase from the above URL"
	promptText := fmt.Sprintf("%s\n%s\n%s\n%s", promptHeader, promptDescription, promptURL, promptQuestion)
	resp, err := promptutil.ValidatePrompt(r, promptText, func(input string) error {
		return promptutil.ValidatePhrase(input, exitPassphrase)
	})
	if err != nil {
		return nil, nil, err
	}
	if strings.ToLower(resp) == "n" {
		return nil, nil, nil
	}

	return rawPubKeys, formattedPubKeys, nil
}

func prepareClients(cliCtx *cli.Context) (*ethpb.BeaconNodeValidatorClient, *ethpb.NodeClient, error) {
	dialOpts := client.ConstructDialOptions(
		cmd.GrpcMaxCallRecvMsgSizeFlag.Value,
		flags.CertFlag.Value,
		strings.Split(flags.GrpcHeadersFlag.Value, ","),
		flags.GrpcRetriesFlag.Value,
		flags.GrpcRetryDelayFlag.Value,
	)
	if dialOpts == nil {
		return nil, nil, errors.New("failed to construct dial options")
	}
	conn, err := grpc.DialContext(cliCtx.Context, cliCtx.String(flags.BeaconRPCProviderFlag.Name), dialOpts...)
	if err != nil {
		return nil, nil, errors.Wrapf(err, "could not dial endpoint %s", flags.BeaconRPCProviderFlag.Name)
	}
	validatorClient := ethpb.NewBeaconNodeValidatorClient(conn)
	nodeClient := ethpb.NewNodeClient(conn)

	return &validatorClient, &nodeClient, nil
}

func performExit(cliCtx *cli.Context, cfg performExitCfg) ([]string, error) {
	var rawNotExitedKeys [][]byte
	for i, key := range cfg.rawPubKeys {
		if err := client.ProposeExit(cliCtx.Context, cfg.validatorClient, cfg.nodeClient, cfg.keymanager.Sign, key); err != nil {
			rawNotExitedKeys = append(rawNotExitedKeys, key)
			log.WithError(err).Errorf("voluntary exit failed for account %s", cfg.formattedPubKeys[i])
		}
	}
	var formattedExitedKeys []string
	for i, key := range cfg.rawPubKeys {
		found := false
		for _, notExited := range rawNotExitedKeys {
			if bytes.Equal(notExited, key) {
				found = true
				break
			}
		}
		if !found {
			formattedExitedKeys = append(formattedExitedKeys, cfg.formattedPubKeys[i])
		}
	}

	return formattedExitedKeys, nil
}
