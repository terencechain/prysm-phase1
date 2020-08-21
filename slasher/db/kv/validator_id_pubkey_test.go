package kv

import (
	"context"
	"flag"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/urfave/cli/v2"
)

type publicKeyTestStruct struct {
	validatorID uint64
	pk          []byte
}

var pkTests []publicKeyTestStruct

func init() {
	pkTests = []publicKeyTestStruct{
		{
			validatorID: 1,
			pk:          []byte{1, 2, 3},
		},
		{
			validatorID: 2,
			pk:          []byte{4, 5, 6},
		},
		{
			validatorID: 3,
			pk:          []byte{7, 8, 9},
		},
	}
}

func TestNilDBValidatorPublicKey(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	validatorID := uint64(1)

	pk, err := db.ValidatorPubKey(ctx, validatorID)
	require.NoError(t, err, "Nil ValidatorPubKey should not return error")
	require.DeepEqual(t, ([]uint8)(nil), pk)
}

func TestSavePubKey(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	for _, tt := range pkTests {
		err := db.SavePubKey(ctx, tt.validatorID, tt.pk)
		require.NoError(t, err, "Save validator public key failed")

		pk, err := db.ValidatorPubKey(ctx, tt.validatorID)
		require.NoError(t, err, "Failed to get validator public key")
		require.NotNil(t, pk)
		require.DeepEqual(t, tt.pk, pk, "Should return validator public key")
	}
}

func TestDeletePublicKey(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	for _, tt := range pkTests {
		require.NoError(t, db.SavePubKey(ctx, tt.validatorID, tt.pk), "Save validator public key failed")
	}

	for _, tt := range pkTests {
		pk, err := db.ValidatorPubKey(ctx, tt.validatorID)
		require.NoError(t, err, "Failed to get validator public key")
		require.NotNil(t, pk)
		require.DeepEqual(t, tt.pk, pk, "Should return validator public key")

		err = db.DeletePubKey(ctx, tt.validatorID)
		require.NoError(t, err, "Delete validator public key")
		pk, err = db.ValidatorPubKey(ctx, tt.validatorID)
		require.NoError(t, err)
		require.DeepEqual(t, []byte(nil), pk, "Expected validator public key to be deleted")
	}
}
