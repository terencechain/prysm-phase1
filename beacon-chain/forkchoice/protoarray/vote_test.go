package protoarray

import (
	"context"
	"testing"

	"github.com/prysmaticlabs/prysm/shared/params"
)

func TestVotes_CanFindHead(t *testing.T) {
	balances := []uint64{1, 1}
	f := setup(1, 1)

	// The head should always start at the finalized block.
	r, err := f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != params.BeaconConfig().ZeroHash {
		t.Errorf("Incorrect head with genesis")
	}

	// Insert block 2 into the tree and verify head is at 2:
	//         0
	//        /
	//       2 <- head
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(2), params.BeaconConfig().ZeroHash, [32]byte{}, 1, 1); err != nil {
		t.Fatal(err)
	}
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(2) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Insert block 1 into the tree and verify head is still at 2:
	//            0
	//           / \
	//  head -> 2  1
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(1), params.BeaconConfig().ZeroHash, [32]byte{}, 1, 1); err != nil {
		t.Fatal(err)
	}
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(2) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Add a vote to block 1 of the tree and verify head is switched to 1:
	//            0
	//           / \
	//          2  1 <- +vote, new head
	f.ProcessAttestation(context.Background(), []uint64{0}, indexToHash(1), 2, [32]byte{}, 0)
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(1) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Add a vote to block 2 of the tree and verify head is switched to 2:
	//                     0
	//                    / \
	// vote, new head -> 2  1
	f.ProcessAttestation(context.Background(), []uint64{1}, indexToHash(2), 2, [32]byte{}, 0)
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(2) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Insert block 3 into the tree and verify head is still at 2:
	//            0
	//           / \
	//  head -> 2  1
	//             |
	//             3
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(3), indexToHash(1), [32]byte{}, 1, 1); err != nil {
		t.Fatal(err)
	}
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(2) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Move validator 0's vote from 1 to 3 and verify head is still at 2:
	//            0
	//           / \
	//  head -> 2  1 <- old vote
	//             |
	//             3 <- new vote
	f.ProcessAttestation(context.Background(), []uint64{0}, indexToHash(3), 3, [32]byte{}, 0)
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(2) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Move validator 1's vote from 2 to 1 and verify head is switched to 3:
	//               0
	//              / \
	// old vote -> 2  1 <- new vote
	//                |
	//                3 <- head
	f.ProcessAttestation(context.Background(), []uint64{1}, indexToHash(1), 3, [32]byte{}, 0)
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(3) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Insert block 4 into the tree and verify head is at 4:
	//            0
	//           / \
	//          2  1
	//             |
	//             3
	//             |
	//             4 <- head
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(4), indexToHash(3), [32]byte{}, 1, 1); err != nil {
		t.Fatal(err)
	}
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(4) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Insert block 5 with justified epoch 2, it should be filtered out:
	//            0
	//           / \
	//          2  1
	//             |
	//             3
	//             |
	//             4 <- head
	//            /
	//           5 <- justified epoch = 2
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(5), indexToHash(4), [32]byte{}, 2, 2); err != nil {
		t.Fatal(err)
	}
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(4) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Insert block 6 with justified epoch 0:
	//            0
	//           / \
	//          2  1
	//             |
	//             3
	//             |
	//             4 <- head
	//            / \
	//           5  6 <- justified epoch = 0
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(6), indexToHash(4), [32]byte{}, 1, 1); err != nil {
		t.Fatal(err)
	}

	// Moved 2 votes to block 5:
	//            0
	//           / \
	//          2  1
	//             |
	//             3
	//             |
	//             4
	//            / \
	// 2 votes-> 5  6
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(6), indexToHash(4), [32]byte{}, 1, 1); err != nil {
		t.Fatal(err)
	}
	f.ProcessAttestation(context.Background(), []uint64{0, 1}, indexToHash(5), 4, [32]byte{}, 00)

	// Inset blocks 7, 8 and 9:
	// 6 should still be the head, even though 5 has all the votes.
	//            0
	//           / \
	//          2  1
	//             |
	//             3
	//             |
	//             4
	//            / \
	//           5  6 <- head
	//           |
	//           7
	//           |
	//           8
	//           |
	//           9
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(7), indexToHash(5), [32]byte{}, 2, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(8), indexToHash(7), [32]byte{}, 2, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(9), indexToHash(8), [32]byte{}, 2, 2); err != nil {
		t.Fatal(err)
	}
	r, err = f.Head(context.Background(), 1, params.BeaconConfig().ZeroHash, balances, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(6) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Update fork choice justified epoch to 1 and start block to 5.
	// Verify 9 is the head:
	//            0
	//           / \
	//          2  1
	//             |
	//             3
	//             |
	//             4
	//            / \
	//           5  6
	//           |
	//           7
	//           |
	//           8
	//           |
	//           9 <- head
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(9) {
		t.Error("Incorrect head for with justified epoch at 2")
	}

	// Insert block 10 and 2 validators updated their vote to 9.
	// Verify 9 is the head:
	//             0
	//            / \
	//           2  1
	//              |
	//              3
	//              |
	//              4
	//             / \
	//            5  6
	//            |
	//            7
	//            |
	//            8
	//           / \
	// 2 votes->9  10
	f.ProcessAttestation(context.Background(), []uint64{0, 1}, indexToHash(9), 5, [32]byte{}, 0)
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(10), indexToHash(8), [32]byte{}, 2, 2); err != nil {
		t.Fatal(err)
	}
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(9) {
		t.Error("Incorrect head for with justified epoch at 2")
	}

	// Add 3 more validators to the system.
	balances = []uint64{1, 1, 1, 1, 1}
	// The new validators voted for 10.
	f.ProcessAttestation(context.Background(), []uint64{2, 3, 4}, indexToHash(10), 5, [32]byte{}, 0)
	// The new head should be 10.
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(10) {
		t.Error("Incorrect head for with justified epoch at 2")
	}

	// Set the balances of the last 2 validators to 0.
	balances = []uint64{1, 1, 1, 0, 0}
	// The head should be back to 9.
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(9) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Set the balances back to normal.
	balances = []uint64{1, 1, 1, 1, 1}
	// The head should be back to 10.
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(10) {
		t.Error("Incorrect head for with justified epoch at 2")
	}

	// Remove the last 2 validators.
	balances = []uint64{1, 1, 1}
	// The head should be back to 9.
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(9) {
		t.Error("Incorrect head for with justified epoch at 1")
	}

	// Verify pruning below the prune threshold does not affect head.
	f.store.PruneThreshold = 1000
	if err := f.store.prune(context.Background(), indexToHash(5)); err != nil {
		t.Fatal(err)
	}
	if len(f.store.Nodes) != 11 {
		t.Error("Incorrect no length after prune")
	}
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(9) {
		t.Error("Incorrect head for with justified epoch at 2")
	}

	// Verify pruning above the prune threshold does prune:
	//          0
	//         / \
	//        2   1
	//            |
	//            3
	//            |
	//            4
	// -------pruned here ------
	//          5   6
	//          |
	//          7
	//          |
	//          8
	//         / \
	//        9  10
	f.store.PruneThreshold = 1
	if err := f.store.prune(context.Background(), indexToHash(5)); err != nil {
		t.Fatal(err)
	}
	if len(f.store.Nodes) != 6 {
		t.Error("Incorrect no length after prune")
	}
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(9) {
		t.Error("Incorrect head for with justified epoch at 2")
	}

	// Insert new block 11 and verify head is at 11.
	//          5   6
	//          |
	//          7
	//          |
	//          8
	//         / \
	//        9  10
	//        |
	// head-> 11
	if err := f.ProcessBlock(context.Background(), 0, indexToHash(11), indexToHash(9), [32]byte{}, 2, 2); err != nil {
		t.Fatal(err)
	}
	r, err = f.Head(context.Background(), 2, indexToHash(5), balances, 2)
	if err != nil {
		t.Fatal(err)
	}
	if r != indexToHash(11) {
		t.Error("Incorrect head for with justified epoch at 2")
	}
}
