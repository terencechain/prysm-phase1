package kv

import (
	"sort"
	"testing"

	fssz "github.com/ferranbt/fastssz"
	c "github.com/patrickmn/go-cache"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
	"github.com/prysmaticlabs/go-bitfield"
	"github.com/prysmaticlabs/prysm/shared/bls"
	"github.com/prysmaticlabs/prysm/shared/testutil/assert"
	"github.com/prysmaticlabs/prysm/shared/testutil/require"
)

func TestKV_Aggregated_AggregateUnaggregatedAttestations(t *testing.T) {
	cache := NewAttCaches()
	priv := bls.RandKey()
	sig1 := priv.Sign([]byte{'a'})
	sig2 := priv.Sign([]byte{'b'})
	att1 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1001}, Signature: sig1.Marshal()}
	att2 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1010}, Signature: sig1.Marshal()}
	att3 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1100}, Signature: sig1.Marshal()}
	att4 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1001}, Signature: sig2.Marshal()}
	att5 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1001}, Signature: sig1.Marshal()}
	att6 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1010}, Signature: sig1.Marshal()}
	att7 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1100}, Signature: sig1.Marshal()}
	att8 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1001}, Signature: sig2.Marshal()}
	atts := []*ethpb.Attestation{att1, att2, att3, att4, att5, att6, att7, att8}
	require.NoError(t, cache.SaveUnaggregatedAttestations(atts))
	require.NoError(t, cache.AggregateUnaggregatedAttestations())

	require.Equal(t, 1, len(cache.AggregatedAttestationsBySlotIndex(1, 0)), "Did not aggregate correctly")
	require.Equal(t, 1, len(cache.AggregatedAttestationsBySlotIndex(2, 0)), "Did not aggregate correctly")
}

func TestKV_Aggregated_SaveAggregatedAttestation(t *testing.T) {
	tests := []struct {
		name          string
		att           *ethpb.Attestation
		count         int
		wantErrString string
	}{
		{
			name: "nil attestation",
			att:  nil,
		},
		{
			name: "nil attestation data",
			att:  &ethpb.Attestation{},
		},
		{
			name: "not aggregated",
			att: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				}, AggregationBits: bitfield.Bitlist{0b10100}},
			wantErrString: "attestation is not aggregated",
		},
		{
			name: "invalid hash",
			att: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					BeaconBlockRoot: []byte{0b0},
				},
				AggregationBits: bitfield.Bitlist{0b10111},
			},
			wantErrString: "could not tree hash attestation: " + fssz.ErrBytesLength.Error(),
		},
		{
			name: "already seen",
			att: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            100,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b11101001},
				Signature:       make([]byte, 96),
			},
			count: 0,
		},
		{
			name: "normal save",
			att: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1101},
				Signature:       make([]byte, 96),
			},
			count: 1,
		},
	}
	r, err := hashFn(&ethpb.AttestationData{
		Slot:            100,
		Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
		Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
		BeaconBlockRoot: make([]byte, 32),
	})
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewAttCaches()
			cache.seenAtt.Set(string(r[:]), []bitfield.Bitlist{{0xff}}, c.DefaultExpiration)
			assert.Equal(t, 0, len(cache.unAggregatedAtt), "Invalid start pool, atts: %d", len(cache.unAggregatedAtt))

			err := cache.SaveAggregatedAttestation(tt.att)
			if tt.wantErrString != "" {
				assert.ErrorContains(t, tt.wantErrString, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.count, len(cache.aggregatedAtt), "Wrong attestation count")
			assert.Equal(t, tt.count, cache.AggregatedAttestationCount(), "Wrong attestation count")
		})
	}
}

func TestKV_Aggregated_SaveAggregatedAttestations(t *testing.T) {
	tests := []struct {
		name          string
		atts          []*ethpb.Attestation
		count         int
		wantErrString string
	}{
		{
			name: "no duplicates",
			atts: []*ethpb.Attestation{
				{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}},
					AggregationBits: bitfield.Bitlist{0b1101}},
				{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}},
					AggregationBits: bitfield.Bitlist{0b1101}},
			},
			count: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewAttCaches()
			assert.Equal(t, 0, len(cache.aggregatedAtt), "Invalid start pool, atts: %d", len(cache.unAggregatedAtt))
			err := cache.SaveAggregatedAttestations(tt.atts)
			if tt.wantErrString != "" {
				assert.ErrorContains(t, tt.wantErrString, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.count, len(cache.aggregatedAtt), "Wrong attestation count")
			assert.Equal(t, tt.count, cache.AggregatedAttestationCount(), "Wrong attestation count")
		})
	}
}

func TestKV_Aggregated_AggregatedAttestations(t *testing.T) {
	cache := NewAttCaches()

	att1 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1101}}
	att2 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1101}}
	att3 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 3, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1101}}
	atts := []*ethpb.Attestation{att1, att2, att3}

	for _, att := range atts {
		require.NoError(t, cache.SaveAggregatedAttestation(att))
	}

	returned := cache.AggregatedAttestations()
	sort.Slice(returned, func(i, j int) bool {
		return returned[i].Data.Slot < returned[j].Data.Slot
	})
	assert.DeepEqual(t, atts, returned)
}

func TestKV_Aggregated_DeleteAggregatedAttestation(t *testing.T) {
	t.Run("nil attestation", func(t *testing.T) {
		cache := NewAttCaches()
		assert.NoError(t, cache.DeleteAggregatedAttestation(nil))
		att := &ethpb.Attestation{AggregationBits: bitfield.Bitlist{0b10101}, Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}}
		assert.NoError(t, cache.DeleteAggregatedAttestation(att))
	})

	t.Run("non aggregated attestation", func(t *testing.T) {
		cache := NewAttCaches()
		att := &ethpb.Attestation{AggregationBits: bitfield.Bitlist{0b1001}, Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}}
		err := cache.DeleteAggregatedAttestation(att)
		assert.ErrorContains(t, "attestation is not aggregated", err)
	})

	t.Run("invalid hash", func(t *testing.T) {
		cache := NewAttCaches()
		att := &ethpb.Attestation{
			AggregationBits: bitfield.Bitlist{0b1111},
			Data: &ethpb.AttestationData{
				Slot: 2,
			},
		}
		err := cache.DeleteAggregatedAttestation(att)
		wantErr := "could not tree hash attestation data: " + fssz.ErrBytesLength.Error()
		assert.ErrorContains(t, wantErr, err)
	})

	t.Run("nonexistent attestation", func(t *testing.T) {
		cache := NewAttCaches()
		att := &ethpb.Attestation{AggregationBits: bitfield.Bitlist{0b1111}, Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}}
		assert.NoError(t, cache.DeleteAggregatedAttestation(att))
	})

	t.Run("non-filtered deletion", func(t *testing.T) {
		cache := NewAttCaches()
		att1 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1101}}
		att2 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1101}}
		att3 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 3, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1101}}
		att4 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 3, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b10101}}
		atts := []*ethpb.Attestation{att1, att2, att3, att4}
		require.NoError(t, cache.SaveAggregatedAttestations(atts))
		require.NoError(t, cache.DeleteAggregatedAttestation(att1))
		require.NoError(t, cache.DeleteAggregatedAttestation(att3))

		returned := cache.AggregatedAttestations()
		wanted := []*ethpb.Attestation{att2}
		assert.DeepEqual(t, wanted, returned)
	})

	t.Run("filtered deletion", func(t *testing.T) {
		cache := NewAttCaches()
		att1 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b110101}}
		att2 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b110111}}
		att3 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b110100}}
		att4 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 2, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b110101}}
		atts := []*ethpb.Attestation{att1, att2, att3, att4}
		require.NoError(t, cache.SaveAggregatedAttestations(atts))

		assert.Equal(t, 2, cache.AggregatedAttestationCount(), "Unexpected number of atts")
		require.NoError(t, cache.DeleteAggregatedAttestation(att4))

		returned := cache.AggregatedAttestations()
		wanted := []*ethpb.Attestation{att1, att2}
		sort.Slice(returned, func(i, j int) bool {
			return string(returned[i].AggregationBits) < string(returned[j].AggregationBits)
		})
		assert.DeepEqual(t, wanted, returned)
	})
}

func TestKV_Aggregated_HasAggregatedAttestation(t *testing.T) {
	tests := []struct {
		name     string
		existing []*ethpb.Attestation
		input    *ethpb.Attestation
		want     bool
	}{
		{
			name:  "nil attestation",
			input: nil,
			want:  false,
		},
		{
			name: "nil attestation data",
			input: &ethpb.Attestation{
				AggregationBits: bitfield.Bitlist{0b1111},
				Data: &ethpb.AttestationData{
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
			},
			want: false,
		},
		{
			name: "empty cache aggregated",
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1111}},
			want: false,
		},
		{
			name: "empty cache unaggregated",
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1001}},
			want: false,
		},
		{
			name: "single attestation in cache with exact match",
			existing: []*ethpb.Attestation{{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1111}},
			},
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1111}},
			want: true,
		},
		{
			name: "single attestation in cache with subset aggregation",
			existing: []*ethpb.Attestation{{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1111}},
			},
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1110}},
			want: true,
		},
		{
			name: "single attestation in cache with superset aggregation",
			existing: []*ethpb.Attestation{{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1110}},
			},
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1111}},
			want: false,
		},
		{
			name: "multiple attestations with same data in cache with overlapping aggregation, input is subset",
			existing: []*ethpb.Attestation{
				{
					Data: &ethpb.AttestationData{
						Slot:            1,
						BeaconBlockRoot: make([]byte, 32),
						Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
						Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					},
					AggregationBits: bitfield.Bitlist{0b1111000},
				},
				{
					Data: &ethpb.AttestationData{
						Slot:            1,
						BeaconBlockRoot: make([]byte, 32),
						Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
						Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					},
					AggregationBits: bitfield.Bitlist{0b1100111},
				},
			},
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1100000}},
			want: true,
		},
		{
			name: "multiple attestations with same data in cache with overlapping aggregation and input is superset",
			existing: []*ethpb.Attestation{
				{
					Data: &ethpb.AttestationData{
						Slot:            1,
						BeaconBlockRoot: make([]byte, 32),
						Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
						Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					},
					AggregationBits: bitfield.Bitlist{0b1111000},
				},
				{
					Data: &ethpb.AttestationData{
						Slot:            1,
						BeaconBlockRoot: make([]byte, 32),
						Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
						Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					},
					AggregationBits: bitfield.Bitlist{0b1100111},
				},
			},
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1111111}},
			want: false,
		},
		{
			name: "multiple attestations with different data in cache",
			existing: []*ethpb.Attestation{
				{
					Data: &ethpb.AttestationData{
						Slot:            2,
						BeaconBlockRoot: make([]byte, 32),
						Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
						Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					},
					AggregationBits: bitfield.Bitlist{0b1111000},
				},
				{
					Data: &ethpb.AttestationData{
						Slot:            3,
						BeaconBlockRoot: make([]byte, 32),
						Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
						Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					},
					AggregationBits: bitfield.Bitlist{0b1100111},
				},
			},
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            1,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1111111}},
			want: false,
		},
		{
			name: "attestations with different bitlist lengths",
			existing: []*ethpb.Attestation{
				{
					Data: &ethpb.AttestationData{
						Slot:            2,
						BeaconBlockRoot: make([]byte, 32),
						Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
						Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					},
					AggregationBits: bitfield.Bitlist{0b1111000},
				},
			},
			input: &ethpb.Attestation{
				Data: &ethpb.AttestationData{
					Slot:            2,
					BeaconBlockRoot: make([]byte, 32),
					Target:          &ethpb.Checkpoint{Root: make([]byte, 32)},
					Source:          &ethpb.Checkpoint{Root: make([]byte, 32)},
				},
				AggregationBits: bitfield.Bitlist{0b1111},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := NewAttCaches()
			require.NoError(t, cache.SaveAggregatedAttestations(tt.existing))

			if tt.input != nil && tt.input.Signature == nil {
				tt.input.Signature = make([]byte, 96)
			}

			result, err := cache.HasAggregatedAttestation(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)

			// Same test for block attestations
			cache = NewAttCaches()
			assert.NoError(t, cache.SaveBlockAttestations(tt.existing))

			result, err = cache.HasAggregatedAttestation(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestKV_Aggregated_DuplicateAggregatedAttestations(t *testing.T) {
	cache := NewAttCaches()

	att1 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1101}}
	att2 := &ethpb.Attestation{Data: &ethpb.AttestationData{Slot: 1, BeaconBlockRoot: make([]byte, 32), Target: &ethpb.Checkpoint{Root: make([]byte, 32)}, Source: &ethpb.Checkpoint{Root: make([]byte, 32)}}, AggregationBits: bitfield.Bitlist{0b1111}}
	atts := []*ethpb.Attestation{att1, att2}

	for _, att := range atts {
		require.NoError(t, cache.SaveAggregatedAttestation(att))
	}

	returned := cache.AggregatedAttestations()

	// It should have only returned att2.
	assert.DeepEqual(t, att2, returned[0], "Did not receive correct aggregated atts")
	assert.Equal(t, 1, len(returned), "Did not receive correct aggregated atts")
}
