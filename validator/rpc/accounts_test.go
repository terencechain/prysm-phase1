package rpc

import (
	"context"
	"testing"

	ptypes "github.com/gogo/protobuf/types"
	pb "github.com/prysmaticlabs/prysm/proto/validator/accounts/v2"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	v2 "github.com/prysmaticlabs/prysm/validator/accounts/v2"
	v2keymanager "github.com/prysmaticlabs/prysm/validator/keymanager/v2"
	"github.com/prysmaticlabs/prysm/validator/keymanager/v2/direct"
)

func TestServer_CreateAccount(t *testing.T) {
	ctx := context.Background()
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	strongPass := "29384283xasjasd32%%&*@*#*"
	// We attempt to create the wallet.
	wallet, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &v2.WalletConfig{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	km, err := wallet.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	s := &Server{
		keymanager:        km,
		walletInitialized: true,
		wallet:            wallet,
	}
	resp, err := s.CreateAccount(ctx, &ptypes.Empty{})
	require.NoError(t, err)
	assert.NotNil(t, resp.Account.ValidatingPublicKey)
}

func TestServer_ListAccounts(t *testing.T) {
	ctx := context.Background()
	localWalletDir := setupWalletDir(t)
	defaultWalletPath = localWalletDir
	strongPass := "29384283xasjasd32%%&*@*#*"
	// We attempt to create the wallet.
	wallet, err := v2.CreateWalletWithKeymanager(ctx, &v2.CreateWalletConfig{
		WalletCfg: &v2.WalletConfig{
			WalletDir:      defaultWalletPath,
			KeymanagerKind: v2keymanager.Direct,
			WalletPassword: strongPass,
		},
		SkipMnemonicConfirm: true,
	})
	require.NoError(t, err)
	km, err := wallet.InitializeKeymanager(ctx, true /* skip mnemonic confirm */)
	require.NoError(t, err)
	s := &Server{
		keymanager:        km,
		walletInitialized: true,
		wallet:            wallet,
	}
	numAccounts := 5
	keys := make([][]byte, numAccounts)
	for i := 0; i < numAccounts; i++ {
		key, err := km.(*direct.Keymanager).CreateAccount(ctx)
		require.NoError(t, err)
		keys[i] = key
	}
	resp, err := s.ListAccounts(ctx, &pb.ListAccountsRequest{})
	require.NoError(t, err)
	require.Equal(t, len(resp.Accounts), numAccounts)
	for i := 0; i < numAccounts; i++ {
		assert.DeepEqual(t, resp.Accounts[i].ValidatingPublicKey, keys[i])
	}
}
