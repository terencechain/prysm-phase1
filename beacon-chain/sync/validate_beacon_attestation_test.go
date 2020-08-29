package sync

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsubpb "github.com/libp2p/go-libp2p-pubsub/pb"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	mockChain "github.com/prysmaticlabs/prysm/beacon-chain/blockchain/testing"
	"github.com/prysmaticlabs/prysm/beacon-chain/cache"
	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	dbtest "github.com/prysmaticlabs/prysm/beacon-chain/db/testing"
	p2ptest "github.com/prysmaticlabs/prysm/beacon-chain/p2p/testing"
	mockSync "github.com/prysmaticlabs/prysm/beacon-chain/sync/initial-sync/testing"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/testutil"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestService_validateCommitteeIndexBeaconAttestation(t *testing.T) {
	ctx := context.Background()
	p := p2ptest.NewTestP2P(t)
	db, _ := dbtest.SetupDB(t)
	chain := &mockChain.ChainService{
		// 1 slot ago.
		Genesis:          time.Now().Add(time.Duration(-1*int64(params.BeaconConfig().SecondsPerSlot)) * time.Second),
		ValidatorsRoot:   [32]byte{'A'},
		ValidAttestation: true,
	}

	c, err := lru.New(10)
	require.NoError(t, err)
	s := &Service{
		initialSync:          &mockSync.Sync{IsSyncing: false},
		p2p:                  p,
		db:                   db,
		chain:                chain,
		blkRootToPendingAtts: make(map[[32]byte][]*ethpb.SignedAggregateAttestationAndProof),
		seenAttestationCache: c,
		stateSummaryCache:    cache.NewStateSummaryCache(),
	}
	err = s.initCaches()
	require.NoError(t, err)

	invalidRoot := [32]byte{'A', 'B', 'C', 'D'}
	s.setBadBlock(ctx, invalidRoot)

	digest, err := s.forkDigest()
	require.NoError(t, err)

	blk := testutil.NewBeaconBlock()
	blk.Block.Slot = 1
	require.NoError(t, db.SaveBlock(ctx, blk))

	validBlockRoot, err := blk.Block.HashTreeRoot()
	require.NoError(t, err)

	validators := uint64(64)
	savedState, keys := testutil.DeterministicGenesisState(t, validators)
	require.NoError(t, savedState.SetSlot(1))
	require.NoError(t, db.SaveState(context.Background(), savedState, validBlockRoot))
	chain.State = savedState

	tests := []struct {
		name                      string
		msg                       *ethpb.Attestation
		topic                     string
		validAttestationSignature bool
		want                      bool
	}{
		{
			name: "valid attestation signature",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      0,
					Slot:                1,
					Target: &ethpb.Checkpoint{
						Epoch: 0,
						Root:  validBlockRoot[:],
					},
					Source: &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      true,
		},
		{
			name: "already seen",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      0,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      false,
		},
		{
			name: "invalid beacon block",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     invalidRoot[:],
					CommitteeIndex:      0,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      false,
		},

		{
			name: "wrong committee index",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      2,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_2", digest),
			validAttestationSignature: true,
			want:                      false,
		},
		{
			name: "already aggregated",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1011},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      1,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      false,
		},
		{
			name: "missing block",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     bytesutil.PadTo([]byte("missing"), 32),
					CommitteeIndex:      1,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      false,
		},
		{
			name: "invalid attestation",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      1,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: false,
			want:                      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain.ValidAttestation = tt.validAttestationSignature
			if tt.validAttestationSignature {
				com, err := helpers.BeaconCommitteeFromState(savedState, tt.msg.Data.Slot, tt.msg.Data.CommitteeIndex)
				require.NoError(t, err)
				domain, err := helpers.Domain(savedState.Fork(), tt.msg.Data.Target.Epoch, params.BeaconConfig().DomainBeaconAttester, savedState.GenesisValidatorRoot())
				require.NoError(t, err)
				attRoot, err := helpers.ComputeSigningRoot(tt.msg.Data, domain)
				require.NoError(t, err)
				for i := 0; ; i++ {
					if tt.msg.AggregationBits.BitAt(uint64(i)) {
						tt.msg.Signature = keys[com[i]].Sign(attRoot[:]).Marshal()
						break
					}
				}
			} else {
				tt.msg.Signature = make([]byte, 96)
			}
			buf := new(bytes.Buffer)
			_, err := p.Encoding().EncodeGossip(buf, tt.msg)
			require.NoError(t, err)
			m := &pubsub.Message{
				Message: &pubsubpb.Message{
					Data:     buf.Bytes(),
					TopicIDs: []string{tt.topic},
				},
			}
			received := s.validateCommitteeIndexBeaconAttestation(ctx, "" /*peerID*/, m) == pubsub.ValidationAccept
			if received != tt.want {
				t.Fatalf("Did not received wanted validation. Got %v, wanted %v", !tt.want, tt.want)
			}
			if tt.want && m.ValidatorData == nil {
				t.Error("Expected validator data to be set")
			}
		})
	}
}

func TestService_validateCommitteeIndexBeaconAttestationUseCheckptCache(t *testing.T) {
	ctx := context.Background()
	p := p2ptest.NewTestP2P(t)
	db, _ := dbtest.SetupDB(t)
	chain := &mockChain.ChainService{
		// 1 slot ago.
		Genesis:          time.Now().Add(time.Duration(-1*int64(params.BeaconConfig().SecondsPerSlot)) * time.Second),
		ValidatorsRoot:   [32]byte{'A'},
		ValidAttestation: true,
	}

	c, err := lru.New(10)
	require.NoError(t, err)
	s := &Service{
		initialSync:          &mockSync.Sync{IsSyncing: false},
		p2p:                  p,
		db:                   db,
		chain:                chain,
		blkRootToPendingAtts: make(map[[32]byte][]*ethpb.SignedAggregateAttestationAndProof),
		seenAttestationCache: c,
		stateSummaryCache:    cache.NewStateSummaryCache(),
	}
	err = s.initCaches()
	require.NoError(t, err)

	invalidRoot := [32]byte{'A', 'B', 'C', 'D'}
	s.setBadBlock(ctx, invalidRoot)

	digest, err := s.forkDigest()
	require.NoError(t, err)

	blk := testutil.NewBeaconBlock()
	blk.Block.Slot = 1
	require.NoError(t, db.SaveBlock(ctx, blk))

	validBlockRoot, err := blk.Block.HashTreeRoot()
	require.NoError(t, err)

	validators := uint64(64)
	savedState, keys := testutil.DeterministicGenesisState(t, validators)
	require.NoError(t, savedState.SetSlot(1))
	require.NoError(t, db.SaveState(context.Background(), savedState, validBlockRoot))
	chain.State = savedState

	tests := []struct {
		name                      string
		msg                       *ethpb.Attestation
		topic                     string
		validAttestationSignature bool
		want                      bool
	}{
		{
			name: "valid attestation signature",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      0,
					Slot:                1,
					Target: &ethpb.Checkpoint{
						Epoch: 0,
						Root:  validBlockRoot[:],
					},
					Source: &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      true,
		},
		{
			name: "already seen",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      0,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      false,
		},
		{
			name: "invalid beacon block",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     invalidRoot[:],
					CommitteeIndex:      0,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      false,
		},

		{
			name: "wrong committee index",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      2,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_2", digest),
			validAttestationSignature: true,
			want:                      false,
		},
		{
			name: "already aggregated",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1011},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      1,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      false,
		},
		{
			name: "missing block",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     bytesutil.PadTo([]byte("missing"), 32),
					CommitteeIndex:      1,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: true,
			want:                      false,
		},
		{
			name: "invalid attestation",
			msg: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1010},
				Data: &ethpb.AttestationData{
					ShardTransitionRoot: make([]byte, 32),
					ShardHeadRoot:       make([]byte, 32),
					BeaconBlockRoot:     validBlockRoot[:],
					CommitteeIndex:      1,
					Slot:                1,
					Target:              &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:              &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			topic:                     fmt.Sprintf("/eth2/%x/beacon_attestation_1", digest),
			validAttestationSignature: false,
			want:                      false,
		},
	}

	resetCfg := featureconfig.InitWithReset(&featureconfig.Flags{UseCheckPointInfoCache: true})
	defer resetCfg()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chain.ValidAttestation = tt.validAttestationSignature
			if tt.validAttestationSignature {
				com, err := helpers.BeaconCommitteeFromState(savedState, tt.msg.Data.Slot, tt.msg.Data.CommitteeIndex)
				require.NoError(t, err)
				domain, err := helpers.Domain(savedState.Fork(), tt.msg.Data.Target.Epoch, params.BeaconConfig().DomainBeaconAttester, savedState.GenesisValidatorRoot())
				require.NoError(t, err)
				attRoot, err := helpers.ComputeSigningRoot(tt.msg.Data, domain)
				require.NoError(t, err)
				for i := 0; ; i++ {
					if tt.msg.AggregationBits.BitAt(uint64(i)) {
						tt.msg.Signature = keys[com[i]].Sign(attRoot[:]).Marshal()
						break
					}
				}
			} else {
				tt.msg.Signature = make([]byte, 96)
			}
			buf := new(bytes.Buffer)
			_, err := p.Encoding().EncodeGossip(buf, tt.msg)
			require.NoError(t, err)
			m := &pubsub.Message{
				Message: &pubsubpb.Message{
					Data:     buf.Bytes(),
					TopicIDs: []string{tt.topic},
				},
			}
			received := s.validateCommitteeIndexBeaconAttestation(ctx, "" /*peerID*/, m) == pubsub.ValidationAccept
			if received != tt.want {
				t.Fatalf("Did not received wanted validation. Got %v, wanted %v", !tt.want, tt.want)
			}
			if tt.want && m.ValidatorData == nil {
				t.Error("Expected validator data to be set")
			}
		})
	}
}
