package state

import (
	mempl "github.com/line/ostracon/mempool"
	"github.com/line/ostracon/types"
)

// TxPreCheck returns a function to filter transactions before processing.
// The function limits the size of a transaction to the block's maximum data size.
func TxPreCheck(state State) mempl.PreCheckFunc {
	maxDataBytes := types.MaxDataBytesNoEvidence(
		state.ConsensusParams.Block.MaxBytes,
		state.Validators.Size(),
	)
	return mempl.PreCheckMaxBytes(maxDataBytes)
}
