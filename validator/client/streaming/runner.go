package streaming

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prysmaticlabs/prysm/beacon-chain/core/helpers"
	"github.com/prysmaticlabs/prysm/shared/bytesutil"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/params"
	"go.opencensus.io/trace"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Validator interface defines the primary methods of a validator client.
type Validator interface {
	Done()
	WaitForChainStart(ctx context.Context) error
	WaitForSync(ctx context.Context) error
	WaitForSynced(ctx context.Context) error
	WaitForActivation(ctx context.Context) error
	NextSlot() <-chan uint64
	CurrentSlot() uint64
	SlotDeadline(slot uint64) time.Time
	LogValidatorGainsAndLosses(ctx context.Context, slot uint64) error
	StreamDuties(ctx context.Context) error
	UpdateProtections(ctx context.Context, slot uint64) error
	RolesAt(ctx context.Context, slot uint64) (map[[48]byte][]validatorRole, error) // validator pubKey -> roles
	SubmitAttestation(ctx context.Context, slot uint64, pubKey [48]byte)
	ProposeBlock(ctx context.Context, slot uint64, pubKey [48]byte)
	SubmitAggregateAndProof(ctx context.Context, slot uint64, pubKey [48]byte)
	LogAttestationsSubmitted()
	SaveProtections(ctx context.Context) error
	UpdateDomainDataCaches(ctx context.Context, slot uint64)
}

// Run the main validator routine. This routine exits if the context is
// canceled.
//
// Order of operations:
// 1 - Initialize validator data
// 2 - Wait for validator activation
// 3 - Listen to a server-side stream of validator duties
// 4 - Wait for the next slot start
// 5 - Determine role at current slot
// 6 - Perform assigned role, if any
func run(ctx context.Context, v Validator) {
	defer v.Done()
	if featureconfig.Get().WaitForSynced {
		if err := v.WaitForSynced(ctx); err != nil {
			log.Fatalf("Could not determine if chain started and beacon node is synced: %v", err)
		}
	} else {
		if err := v.WaitForChainStart(ctx); err != nil {
			log.Fatalf("Could not determine if beacon chain started: %v", err)
		}
		if err := v.WaitForSync(ctx); err != nil {
			log.Fatalf("Could not determine if beacon node synced: %v", err)
		}
	}
	if err := v.WaitForActivation(ctx); err != nil {
		log.Fatalf("Could not wait for validator activation: %v", err)
	}
	// We listen to a server-side stream of validator duties in the
	// background of the validator client.
	go func() {
		if err := v.StreamDuties(ctx); err != nil {
			handleAssignmentError(err, v.CurrentSlot())
		}
	}()
	for {
		ctx, span := trace.StartSpan(ctx, "validator.processSlot")

		select {
		case <-ctx.Done():
			log.Info("Context canceled, stopping validator")
			return // Exit if context is canceled.
		case slot := <-v.NextSlot():
			span.AddAttributes(trace.Int64Attribute("slot", int64(slot)))
			deadline := v.SlotDeadline(slot)
			slotCtx, _ := context.WithDeadline(ctx, deadline)
			// Report this validator client's rewards and penalties throughout its lifecycle.
			log := log.WithField("slot", slot)
			log.WithField("deadline", deadline).Debug("Set deadline for proposals and attestations")
			if err := v.LogValidatorGainsAndLosses(slotCtx, slot); err != nil {
				log.WithError(err).Error("Could not report validator's rewards/penalties")
			}

			if featureconfig.Get().ProtectAttester {
				if err := v.UpdateProtections(ctx, slot); err != nil {
					log.WithError(err).Error("Could not update validator protection")
				}
			}

			// Start fetching domain data for the next epoch.
			if helpers.IsEpochEnd(slot) {
				go v.UpdateDomainDataCaches(ctx, slot+1)
			}

			var wg sync.WaitGroup

			allRoles, err := v.RolesAt(ctx, slot)
			if err != nil {
				log.WithError(err).Error("Could not get validator roles")
				continue
			}
			for id, roles := range allRoles {
				wg.Add(len(roles))
				for _, role := range roles {
					go func(role validatorRole, id [48]byte) {
						defer wg.Done()
						switch role {
						case roleAttester:
							v.SubmitAttestation(slotCtx, slot, id)
						case roleProposer:
							v.ProposeBlock(slotCtx, slot, id)
						case roleAggregator:
							v.SubmitAggregateAndProof(slotCtx, slot, id)
						case roleUnknown:
							log.WithField("pubKey", fmt.Sprintf("%#x", bytesutil.Trunc(id[:]))).Trace("No active roles, doing nothing")
						default:
							log.Warnf("Unhandled role %v", role)
						}
					}(role, id)
				}
			}
			// Wait for all processes to complete, then report span complete.
			go func() {
				wg.Wait()
				v.LogAttestationsSubmitted()
				if featureconfig.Get().ProtectAttester {
					if err := v.SaveProtections(ctx); err != nil {
						log.WithError(err).Error("Could not save validator protection")
					}
				}
				span.End()
			}()
		}
	}
}

func handleAssignmentError(err error, slot uint64) {
	if errCode, ok := status.FromError(err); ok && errCode.Code() == codes.NotFound {
		log.WithField(
			"epoch", slot/params.BeaconConfig().SlotsPerEpoch,
		).Warn("Validator not yet assigned to epoch")
	} else {
		log.WithField("error", err).Error("Failed to update assignments")
	}
}
