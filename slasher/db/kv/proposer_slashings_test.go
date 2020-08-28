package kv

import (
	"context"
	"flag"
	"reflect"
	"sort"
	"testing"

	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
	"github.com/prysmaticlabs/prysm/slasher/db/types"
	"github.com/urfave/cli/v2"
	"gopkg.in/d4l3k/messagediff.v1"
)

func TestStore_ProposerSlashingNilBucket(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	ps := &ethpb.ProposerSlashing{
		Header_1: &ethpb.SignedBeaconBlockHeader{
			Header: &ethpb.BeaconBlockHeader{
				ProposerIndex: 1,
				ParentRoot:    make([]byte, 32),
				StateRoot:     make([]byte, 32),
				BodyRoot:      make([]byte, 32),
			},
			Signature: make([]byte, 96),
		},
		Header_2: &ethpb.SignedBeaconBlockHeader{
			Header: &ethpb.BeaconBlockHeader{
				ProposerIndex: 1,
				ParentRoot:    make([]byte, 32),
				StateRoot:     make([]byte, 32),
				BodyRoot:      make([]byte, 32),
			},
			Signature: make([]byte, 96),
		},
	}
	has, _, err := db.HasProposerSlashing(ctx, ps)
	require.NoError(t, err)
	require.Equal(t, false, has)

	p, err := db.ProposalSlashingsByStatus(ctx, types.SlashingStatus(types.Active))
	require.NoError(t, err, "Failed to get proposer slashing")
	require.NotNil(t, p)
	require.Equal(t, 0, len(p), "Get should return empty attester slashing array for a non existent key")
}

func TestStore_SaveProposerSlashing(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	tests := []struct {
		ss types.SlashingStatus
		ps *ethpb.ProposerSlashing
	}{
		{
			ss: types.Active,
			ps: &ethpb.ProposerSlashing{
				Header_1: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 1,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
				Header_2: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 1,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
			},
		},
		{
			ss: types.Included,
			ps: &ethpb.ProposerSlashing{
				Header_1: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 2,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
				Header_2: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 2,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
			},
		},
		{
			ss: types.Reverted,
			ps: &ethpb.ProposerSlashing{
				Header_1: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 3,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
				Header_2: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 3,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
			},
		},
	}

	for _, tt := range tests {
		err := db.SaveProposerSlashing(ctx, tt.ss, tt.ps)
		require.NoError(t, err, "Save proposer slashing failed")

		proposerSlashings, err := db.ProposalSlashingsByStatus(ctx, tt.ss)
		require.NoError(t, err, "Failed to get proposer slashings")

		if proposerSlashings == nil || !reflect.DeepEqual(proposerSlashings[0], tt.ps) {
			diff, _ := messagediff.PrettyDiff(proposerSlashings[0], tt.ps)
			t.Log(diff)
			t.Fatalf("Proposer slashing: %v should be part of proposer slashings response: %v", tt.ps, proposerSlashings)
		}
	}
}

func TestStore_UpdateProposerSlashingStatus(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	tests := []struct {
		ss types.SlashingStatus
		ps *ethpb.ProposerSlashing
	}{
		{
			ss: types.Active,
			ps: &ethpb.ProposerSlashing{
				Header_1: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 1,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
				Header_2: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 1,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
			},
		},
		{
			ss: types.Active,
			ps: &ethpb.ProposerSlashing{
				Header_1: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 2,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
				Header_2: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 2,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
			},
		},
		{
			ss: types.Active,
			ps: &ethpb.ProposerSlashing{
				Header_1: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 3,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
				Header_2: &ethpb.SignedBeaconBlockHeader{
					Header: &ethpb.BeaconBlockHeader{
						ProposerIndex: 3,
						ParentRoot:    make([]byte, 32),
						StateRoot:     make([]byte, 32),
						BodyRoot:      make([]byte, 32),
					},
					Signature: make([]byte, 96),
				},
			},
		},
	}

	for _, tt := range tests {
		err := db.SaveProposerSlashing(ctx, tt.ss, tt.ps)
		require.NoError(t, err, "Save proposer slashing failed")
	}

	for _, tt := range tests {
		has, st, err := db.HasProposerSlashing(ctx, tt.ps)
		require.NoError(t, err, "Failed to get proposer slashing")
		require.Equal(t, true, has, "Failed to find proposer slashing")
		require.Equal(t, tt.ss, st, "Failed to find proposer slashing with the correct status")

		err = db.SaveProposerSlashing(ctx, types.SlashingStatus(types.Included), tt.ps)
		has, st, err = db.HasProposerSlashing(ctx, tt.ps)
		require.NoError(t, err, "Failed to get proposer slashing")
		require.Equal(t, true, has, "Failed to find proposer slashing")
		require.Equal(t, (types.SlashingStatus)(types.Included), st, "Failed to find proposer slashing with the correct status")
	}
}

func TestStore_SaveProposerSlashings(t *testing.T) {
	app := cli.App{}
	set := flag.NewFlagSet("test", 0)
	db := setupDB(t, cli.NewContext(&app, set, nil))
	ctx := context.Background()

	ps := []*ethpb.ProposerSlashing{
		{
			Header_1: &ethpb.SignedBeaconBlockHeader{
				Header: &ethpb.BeaconBlockHeader{
					ProposerIndex: 1,
					ParentRoot:    make([]byte, 32),
					StateRoot:     make([]byte, 32),
					BodyRoot:      make([]byte, 32),
				},
				Signature: make([]byte, 96),
			},
			Header_2: &ethpb.SignedBeaconBlockHeader{
				Header: &ethpb.BeaconBlockHeader{
					ProposerIndex: 1,
					ParentRoot:    make([]byte, 32),
					StateRoot:     make([]byte, 32),
					BodyRoot:      make([]byte, 32),
				},
				Signature: make([]byte, 96),
			},
		},
		{
			Header_1: &ethpb.SignedBeaconBlockHeader{
				Header: &ethpb.BeaconBlockHeader{
					ProposerIndex: 2,
					ParentRoot:    make([]byte, 32),
					StateRoot:     make([]byte, 32),
					BodyRoot:      make([]byte, 32),
				},
				Signature: make([]byte, 96),
			},
			Header_2: &ethpb.SignedBeaconBlockHeader{
				Header: &ethpb.BeaconBlockHeader{
					ProposerIndex: 2,
					ParentRoot:    make([]byte, 32),
					StateRoot:     make([]byte, 32),
					BodyRoot:      make([]byte, 32),
				},
				Signature: make([]byte, 96),
			},
		},
		{
			Header_1: &ethpb.SignedBeaconBlockHeader{
				Header: &ethpb.BeaconBlockHeader{
					ProposerIndex: 3,
					ParentRoot:    make([]byte, 32),
					StateRoot:     make([]byte, 32),
					BodyRoot:      make([]byte, 32),
				},
				Signature: make([]byte, 96),
			},
			Header_2: &ethpb.SignedBeaconBlockHeader{
				Header: &ethpb.BeaconBlockHeader{
					ProposerIndex: 3,
					ParentRoot:    make([]byte, 32),
					StateRoot:     make([]byte, 32),
					BodyRoot:      make([]byte, 32),
				},
				Signature: make([]byte, 96),
			},
		},
	}
	err := db.SaveProposerSlashings(ctx, types.Active, ps)
	require.NoError(t, err, "Save proposer slashings failed")
	proposerSlashings, err := db.ProposalSlashingsByStatus(ctx, types.Active)
	require.NoError(t, err, "Failed to get proposer slashings")
	sort.SliceStable(proposerSlashings, func(i, j int) bool {
		return proposerSlashings[i].Header_1.Header.ProposerIndex < proposerSlashings[j].Header_1.Header.ProposerIndex
	})
	if proposerSlashings == nil || !reflect.DeepEqual(proposerSlashings, ps) {
		diff, _ := messagediff.PrettyDiff(proposerSlashings, ps)
		t.Log(diff)
		t.Fatalf("Proposer slashing: %v should be part of proposer slashings response: %v", ps, proposerSlashings)
	}
}
