// Package depositutil contains useful functions for dealing
// with eth2 deposit inputs.
package depositutil

import (
	"fmt"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-ssz"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/beacon-chain/state"
	contract "github.com/prysmaticlabs/prysm/contracts/deposit-contract"
	p2ppb "github.com/prysmaticlabs/prysm/proto/beacon/p2p/v1"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/sirupsen/logrus"
)

// DepositInput for a given key. This input data can be used to when making a
// validator deposit. The input data includes a proof of possession field
// signed by the deposit key.
//
// Spec details about general deposit workflow:
//   To submit a deposit:
//
//   - Pack the validator's initialization parameters into deposit_data, a Deposit_Data SSZ object.
//   - Let amount be the amount in Gwei to be deposited by the validator where MIN_DEPOSIT_AMOUNT <= amount <= MAX_EFFECTIVE_BALANCE.
//   - Set deposit_data.amount = amount.
//   - Let signature be the result of bls_sign of the signing_root(deposit_data) with domain=compute_domain(DOMAIN_DEPOSIT). (Deposits are valid regardless of fork version, compute_domain will default to zeroes there).
//   - Send a transaction on the Ethereum 1.0 chain to DEPOSIT_CONTRACT_ADDRESS executing def deposit(pubkey: bytes[48], withdrawal_credentials: bytes[32], signature: bytes[96]) along with a deposit of amount Gwei.
//
// See: https://github.com/ethereum/eth2.0-specs/blob/master/specs/validator/0_beacon-chain-validator.md#submit-deposit
func DepositInput(
	depositKey bls.SecretKey,
	withdrawalKey bls.SecretKey,
	amountInGwei uint64,
) (*ethpb.Deposit_Data, [32]byte, error) {
	di := &ethpb.Deposit_Data{
		PublicKey:             depositKey.PublicKey().Marshal(),
		WithdrawalCredentials: WithdrawalCredentialsHash(withdrawalKey),
		Amount:                amountInGwei,
	}

	sr, err := ssz.SigningRoot(di)
	if err != nil {
		return nil, [32]byte{}, err
	}

	domain, err := helpers.ComputeDomain(
		params.BeaconConfig().DomainDeposit,
		nil, /*forkVersion*/
		nil, /*genesisValidatorsRoot*/
	)
	if err != nil {
		return nil, [32]byte{}, err
	}
	root, err := (&p2ppb.SigningData{ObjectRoot: sr[:], Domain: domain}).HashTreeRoot()
	if err != nil {
		return nil, [32]byte{}, err
	}
	di.Signature = depositKey.Sign(root[:]).Marshal()

	dr, err := di.HashTreeRoot()
	if err != nil {
		return nil, [32]byte{}, err
	}

	return di, dr, nil
}

// WithdrawalCredentialsHash forms a 32 byte hash of the withdrawal public
// address.
//
// The specification is as follows:
//   withdrawal_credentials[:1] == BLS_WITHDRAWAL_PREFIX_BYTE
//   withdrawal_credentials[1:] == hash(withdrawal_pubkey)[1:]
// where withdrawal_credentials is of type bytes32.
func WithdrawalCredentialsHash(withdrawalKey bls.SecretKey) []byte {
	h := hashutil.Hash(withdrawalKey.PublicKey().Marshal())
	return append([]byte{params.BeaconConfig().BLSWithdrawalPrefixByte}, h[1:]...)[:32]
}

// VerifyDepositSignature verifies the correctness of Eth1 deposit BLS signature
func VerifyDepositSignature(dd *ethpb.Deposit_Data, domain []byte) error {
	if featureconfig.Get().SkipBLSVerify {
		return nil
	}
	ddCopy := state.CopyDepositData(dd)
	publicKey, err := bls.PublicKeyFromBytes(dd.PublicKey)
	if err != nil {
		return errors.Wrap(err, "could not convert bytes to public key")
	}
	sig, err := bls.SignatureFromBytes(dd.Signature)
	if err != nil {
		return errors.Wrap(err, "could not convert bytes to signature")
	}
	ddCopy.Signature = nil
	root, err := ssz.SigningRoot(ddCopy)
	if err != nil {
		return errors.Wrap(err, "could not get signing root")
	}
	signingData := &p2ppb.SigningData{
		ObjectRoot: root[:],
		Domain:     domain,
	}
	ctrRoot, err := signingData.HashTreeRoot()
	if err != nil {
		return errors.Wrap(err, "could not get container root")
	}
	if !sig.Verify(publicKey, ctrRoot[:]) {
		return helpers.ErrSigFailedToVerify
	}
	return nil
}

// GenerateDepositTransaction uses the provided validating key and withdrawal key to
// create a transaction object for the deposit contract.
func GenerateDepositTransaction(
	validatingKey bls.SecretKey,
	withdrawalKey bls.SecretKey,
) (*types.Transaction, *ethpb.Deposit_Data, error) {
	depositData, depositRoot, err := DepositInput(
		validatingKey, withdrawalKey, params.BeaconConfig().MaxEffectiveBalance,
	)
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not generate deposit input")
	}
	testAcc, err := contract.Setup()
	if err != nil {
		return nil, nil, errors.Wrap(err, "could not load deposit contract")
	}
	testAcc.TxOpts.GasLimit = 1000000

	tx, err := testAcc.Contract.Deposit(
		testAcc.TxOpts,
		depositData.PublicKey,
		depositData.WithdrawalCredentials,
		depositData.Signature,
		depositRoot,
	)
	return tx, depositData, nil
}

// LogDepositTransaction outputs a formatted transaction data to the terminal.
func LogDepositTransaction(log *logrus.Entry, tx *types.Transaction) {
	log.Info(
		"Copy + paste the deposit data below when using the " +
			"eth1 deposit contract")
	fmt.Printf(`
========================Deposit Data===============================

%#x

===================================================================`, tx.Data())
	fmt.Printf(`
***Enter the above deposit data into step 3 on https://prylabs.net/participate***
`)
}
