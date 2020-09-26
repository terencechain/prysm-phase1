package v2

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gogo/protobuf/types"
	"github.com/golang/mock/gomock"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/mock"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/validator/accounts/v2/wallet"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
)

func TestExitAccountsCli_Ok(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockValidatorClient := mock.NewMockBeaconNodeValidatorClient(ctrl)
	mockNodeClient := mock.NewMockNodeClient(ctrl)

	mockValidatorClient.EXPECT().
		ValidatorIndex(gomock.Any(), gomock.Any()).
		Return(&ethpb.ValidatorIndexResponse{Index: 1}, nil)

	// Any time in the past will suffice
	genesisTime := &types.Timestamp{
		Seconds: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
	}

	mockNodeClient.EXPECT().
		GetGenesis(gomock.Any(), gomock.Any()).
		Return(&ethpb.Genesis{GenesisTime: genesisTime}, nil)

	mockValidatorClient.EXPECT().
		DomainData(gomock.Any(), gomock.Any()).
		Return(&ethpb.DomainResponse{SignatureDomain: make([]byte, 32)}, nil)

	mockValidatorClient.EXPECT().
		ProposeExit(gomock.Any(), gomock.AssignableToTypeOf(&ethpb.SignedVoluntaryExit{})).
		Return(&ethpb.ProposeExitResponse{}, nil)

	walletDir, _, passwordFilePath := setupWalletAndPasswordsDir(t)
	randPath, err := rand.Int(rand.Reader, big.NewInt(1000000))
	require.NoError(t, err, "Could not generate random file path")
	// Write a directory where we will import keys from.
	keysDir := filepath.Join(testutil.TempDir(), fmt.Sprintf("/%d", randPath), "keysDir")
	require.NoError(t, os.MkdirAll(keysDir, os.ModePerm))

	// Create keystore file in the keys directory we can then import from in our wallet.
	keystore, _ := createKeystore(t, keysDir)
	time.Sleep(time.Second)

	// We initialize a wallet with a direct keymanager.
	cliCtx := setupWalletCtx(t, &testWalletConfig{
		// Wallet configuration flags.
		walletDir:           walletDir,
		keymanagerKind:      v2keymanager.Direct,
		walletPasswordFile:  passwordFilePath,
		accountPasswordFile: passwordFilePath,
		// Flag required for ImportAccounts to work.
		keysDir: keysDir,
		// Flag required for ExitAccounts to work.
		voluntaryExitPublicKeys: keystore.Pubkey,
	})
	_, err = CreateWalletWithKeymanager(cliCtx.Context, &CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      walletDir,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: "Passwordz0320$",
		},
	})
	require.NoError(t, err)
	require.NoError(t, ImportAccountsCli(cliCtx))

	validatingPublicKeys, keymanager, err := prepareWallet(cliCtx)
	require.NoError(t, err)
	require.NotNil(t, validatingPublicKeys)
	require.NotNil(t, keymanager)

	// Prepare user input for final confirmation step
	var stdin bytes.Buffer
	stdin.Write([]byte(exitPassphrase))
	rawPubKeys, formattedPubKeys, err := interact(cliCtx, &stdin, validatingPublicKeys)
	require.NoError(t, err)
	require.NotNil(t, rawPubKeys)
	require.NotNil(t, formattedPubKeys)

	cfg := performExitCfg{
		mockValidatorClient,
		mockNodeClient,
		keymanager,
		rawPubKeys,
		formattedPubKeys,
	}
	formattedExitedKeys, err := performExit(cliCtx, cfg)
	require.NoError(t, err)
	assert.Equal(t, 1, len(formattedExitedKeys))
	expectedKey := "0x" + keystore.Pubkey[:12]
	assert.Equal(t, expectedKey, formattedExitedKeys[0])
}

func TestPrepareWallet_EmptyWalletReturnsError(t *testing.T) {
	walletDir, _, passwordFilePath := setupWalletAndPasswordsDir(t)
	cliCtx := setupWalletCtx(t, &testWalletConfig{
		walletDir:           walletDir,
		keymanagerKind:      v2keymanager.Direct,
		walletPasswordFile:  passwordFilePath,
		accountPasswordFile: passwordFilePath,
	})
	_, err := CreateWalletWithKeymanager(cliCtx.Context, &CreateWalletConfig{
		WalletCfg: &wallet.Config{
			WalletDir:      walletDir,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: "Passwordz0320$",
		},
	})
	require.NoError(t, err)
	_, _, err = prepareWallet(cliCtx)
	assert.ErrorContains(t, "please recreate your wallet", err)
}
