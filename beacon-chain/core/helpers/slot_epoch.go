package helpers

import (
	"fmt"
	"time"

	"github.com/pkg/errors"
	stateTrie "github.com/prysmaticlabs/prysm/beacon-chain/state"
	"github.com/prysmaticlabs/prysm/shared/mathutil"
	"github.com/prysmaticlabs/prysm/shared/params"
	"github.com/prysmaticlabs/prysm/shared/roughtime"
)

// MaxSlotBuffer specifies the max buffer given to slots from
// incoming objects. (24 mins with mainnet spec)
const MaxSlotBuffer = uint64(1 << 7)

// SlotToEpoch returns the epoch number of the input slot.
//
// Spec pseudocode definition:
//  def compute_epoch_of_slot(slot: Slot) -> Epoch:
//    """
//    Return the epoch number of ``slot``.
//    """
//    return Epoch(slot // SLOTS_PER_EPOCH)
func SlotToEpoch(slot uint64) uint64 {
	return slot / params.BeaconConfig().SlotsPerEpoch
}

// CurrentEpoch returns the current epoch number calculated from
// the slot number stored in beacon state.
//
// Spec pseudocode definition:
//  def get_current_epoch(state: BeaconState) -> Epoch:
//    """
//    Return the current epoch.
//    """
//    return compute_epoch_of_slot(state.slot)
func CurrentEpoch(state *stateTrie.BeaconState) uint64 {
	return SlotToEpoch(state.Slot())
}

// PrevEpoch returns the previous epoch number calculated from
// the slot number stored in beacon state. It also checks for
// underflow condition.
//
// Spec pseudocode definition:
//  def get_previous_epoch(state: BeaconState) -> Epoch:
//    """`
//    Return the previous epoch (unless the current epoch is ``GENESIS_EPOCH``).
//    """
//    current_epoch = get_current_epoch(state)
//    return GENESIS_EPOCH if current_epoch == GENESIS_EPOCH else Epoch(current_epoch - 1)
func PrevEpoch(state *stateTrie.BeaconState) uint64 {
	currentEpoch := CurrentEpoch(state)
	if currentEpoch == 0 {
		return 0
	}
	return currentEpoch - 1
}

// NextEpoch returns the next epoch number calculated from
// the slot number stored in beacon state.
func NextEpoch(state *stateTrie.BeaconState) uint64 {
	return SlotToEpoch(state.Slot()) + 1
}

// StartSlot returns the first slot number of the
// current epoch.
//
// Spec pseudocode definition:
//  def compute_start_slot_at_epoch(epoch: Epoch) -> Slot:
//    """
//    Return the start slot of ``epoch``.
//    """
//    return Slot(epoch * SLOTS_PER_EPOCH)
func StartSlot(epoch uint64) (uint64, error) {
	slot, err := mathutil.Mul64(epoch, params.BeaconConfig().SlotsPerEpoch)
	if err != nil {
		return slot, errors.Errorf("start slot calculation overflows: %v", err)
	}
	return slot, nil
}

// IsEpochStart returns true if the given slot number is an epoch starting slot
// number.
func IsEpochStart(slot uint64) bool {
	return slot%params.BeaconConfig().SlotsPerEpoch == 0
}

// IsEpochEnd returns true if the given slot number is an epoch ending slot
// number.
func IsEpochEnd(slot uint64) bool {
	return IsEpochStart(slot + 1)
}

// SlotsSinceEpochStarts returns number of slots since the start of the epoch.
func SlotsSinceEpochStarts(slot uint64) uint64 {
	return slot % params.BeaconConfig().SlotsPerEpoch
}

// VerifySlotTime validates the input slot is not from the future.
func VerifySlotTime(genesisTime uint64, slot uint64, timeTolerance time.Duration) error {
	slotTime, err := SlotToTime(genesisTime, slot)
	if err != nil {
		return err
	}

	// Defensive check to ensure unreasonable slots are rejected
	// straight away.
	if err := ValidateSlotClock(slot, genesisTime); err != nil {
		return err
	}

	currentTime := roughtime.Now()
	diff := slotTime.Sub(currentTime)

	if diff > timeTolerance {
		return fmt.Errorf("could not process slot from the future, slot time %s > current time %s", slotTime, currentTime)
	}
	return nil
}

// SlotToTime takes the given slot and genesis time to determine the start time of the slot.
func SlotToTime(genesisTimeSec uint64, slot uint64) (time.Time, error) {
	timeSinceGenesis, err := mathutil.Mul64(slot, params.BeaconConfig().SecondsPerSlot)
	if err != nil {
		return time.Unix(0, 0), fmt.Errorf("slot (%d) is in the far distant future: %v", slot, err)
	}
	sTime, err := mathutil.Add64(genesisTimeSec, timeSinceGenesis)
	if err != nil {
		return time.Unix(0, 0), fmt.Errorf("slot (%d) is in the far distant future: %v", slot, err)
	}
	return time.Unix(int64(sTime), 0), nil
}

// SlotsSince computes the number of time slots that have occurred since the given timestamp.
func SlotsSince(time time.Time) uint64 {
	return CurrentSlot(uint64(time.Unix()))
}

// CurrentSlot returns the current slot as determined by the local clock and
// provided genesis time.
func CurrentSlot(genesisTimeSec uint64) uint64 {
	now := roughtime.Now().Unix()
	genesis := int64(genesisTimeSec)
	if now < genesis {
		return 0
	}
	return uint64(now-genesis) / params.BeaconConfig().SecondsPerSlot
}

// ValidateSlotClock validates a provided slot against the local
// clock to ensure slots that are unreasonable are returned with
// an error.
func ValidateSlotClock(slot uint64, genesisTimeSec uint64) error {
	maxPossibleSlot := CurrentSlot(genesisTimeSec) + MaxSlotBuffer
	// Defensive check to ensure that we only process slots up to a hard limit
	// from our local clock.
	if slot > maxPossibleSlot {
		return fmt.Errorf("slot %d > %d which exceeds max allowed value relative to the local clock", slot, maxPossibleSlot)
	}
	return nil
}

// RoundUpToNearestEpoch rounds up the provided slot value to the nearest epoch.
func RoundUpToNearestEpoch(slot uint64) uint64 {
	if slot%params.BeaconConfig().SlotsPerEpoch != 0 {
		slot -= slot % params.BeaconConfig().SlotsPerEpoch
		slot += params.BeaconConfig().SlotsPerEpoch
	}
	return slot
}

// PrevSlot returns previous slot, with an exception in slot 0 to prevent underflow.
//
// Spec code:
// def compute_previous_slot(slot: Slot) -> Slot:
//    if slot > 0:
//        return Slot(slot - 1)
//    else:
//        return Slot(0)
func PrevSlot(slot uint64) uint64 {
	if slot > 0 {
		return slot - 1
	}
	return 0
}

// ComputeOffsetSlots returns the offset slot given the start slot and the end slot.
//
// Spec code:
// def compute_offset_slots(start_slot: Slot, end_slot: Slot) -> Sequence[Slot]:
//    """
//    Return the offset slots for shard blocks up to a particular `end_slot`,
//    assuming the most recent shard state was at slot `start_slot`
//    """
//    return [slot for slot in compute_admissible_slots(start_slot) if slot < end_slot]
func ComputeOffsetSlots(startSlot uint64, endSlot uint64) []uint64 {
	slots := make([]uint64, 0, len(params.BeaconConfig().ShardBlockOffsets))
	aSlots := ComputeAdmissibleSlots(startSlot)
	for _, s := range aSlots {
		if s >= endSlot {
			break
		}
		slots = append(slots, s)
	}

	return slots
}

// ComputeAdmissibleSlots returns the admissible slots.
//
// Spec code:
// def compute_admissible_slots(start_slot: Slot) -> Sequence[Slot]:
//    """
//    Return the admissible slots for shard blocks, assuming the most recent shard state
//    was at slot `start_slot`
//    """
//    return [Slot(start_slot + x) for x in SHARD_BLOCK_OFFSETS]
func ComputeAdmissibleSlots(slot uint64) []uint64 {
	slots := make([]uint64, len(params.BeaconConfig().ShardBlockOffsets))
	for i := 0; i < len(slots); i++ {
		slots[i] =  slot + params.BeaconConfig().ShardBlockOffsets[i]
	}
	return slots
}

// ComputeSourceEpoch returns epoch at the start of the previous period.
// This is used to facilitate computing shard proposer committees and light client committees.
//
// Spec code:
// def compute_committee_source_epoch(epoch: Epoch, period: uint64) -> Epoch:
//    """
//    Return the source epoch for computing the committee.
//    """
//    source_epoch = epoch - epoch % period
//    if source_epoch >= period:
//        source_epoch -= period  # `period` epochs lookahead
//    return source_epoch
func SourceEpoch(epoch uint64, period uint64) uint64 {
	s := epoch - epoch%period
	if s >= period {
		s -= period
	}
	return s
}
