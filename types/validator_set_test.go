package types

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"testing"
	"testing/quick"
	"time"

	tmtime "github.com/tendermint/tendermint/types/time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	tmmath "github.com/tendermint/tendermint/libs/math"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestMaxVotingPowerTest(t *testing.T) {
	large := MaxTotalStakingPower
	maxDiff := int64(0)
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			testNum := (large - int64(i)) >> j
			casted := int64(float64(testNum))
			t.Logf("org=%d, casting=%d", testNum, casted)
			if maxDiff < casted-testNum {
				maxDiff = casted - testNum
			}
		}
	}
	t.Logf("max difference=%d", maxDiff)
	assert.True(t, MaxTotalStakingPower+maxDiff <= MaxTotalVotingPower)
}

func TestValidatorSetBasic(t *testing.T) {
	// empty or nil validator lists are allowed,
	// but attempting to IncrementProposerPriority on them will panic.
	vset := NewValidatorSet([]*Validator{})
	assert.Panics(t, func() { vset.IncrementProposerPriority(1) })
	assert.Panics(t, func() { vset.SelectProposer([]byte{}, 1, 0) })

	vset = NewValidatorSet(nil)
	assert.Panics(t, func() { vset.IncrementProposerPriority(1) })
	assert.Panics(t, func() { vset.SelectProposer([]byte{}, 1, 0) })

	assert.EqualValues(t, vset, vset.Copy())
	assert.False(t, vset.HasAddress([]byte("some val")))
	idx, val := vset.GetByAddress([]byte("some val"))
	assert.EqualValues(t, -1, idx)
	assert.Nil(t, val)
	addr, val := vset.GetByIndex(-100)
	assert.Nil(t, addr)
	assert.Nil(t, val)
	addr, val = vset.GetByIndex(0)
	assert.Nil(t, addr)
	assert.Nil(t, val)
	addr, val = vset.GetByIndex(100)
	assert.Nil(t, addr)
	assert.Nil(t, val)
	assert.Zero(t, vset.Size())
	assert.Equal(t, int64(0), vset.TotalStakingPower())
	assert.Equal(t, []byte{0xe3, 0xb0, 0xc4, 0x42, 0x98, 0xfc, 0x1c, 0x14, 0x9a, 0xfb, 0xf4,
		0xc8, 0x99, 0x6f, 0xb9, 0x24, 0x27, 0xae, 0x41, 0xe4, 0x64, 0x9b, 0x93, 0x4c, 0xa4, 0x95,
		0x99, 0x1b, 0x78, 0x52, 0xb8, 0x55}, vset.Hash())
	// add
	val = randValidator(vset.TotalStakingPower())
	assert.NoError(t, vset.UpdateWithChangeSet([]*Validator{val}))

	assert.True(t, vset.HasAddress(val.Address))
	idx, _ = vset.GetByAddress(val.Address)
	assert.EqualValues(t, 0, idx)
	addr, _ = vset.GetByIndex(0)
	assert.Equal(t, []byte(val.Address), addr)
	assert.Equal(t, 1, vset.Size())
	assert.Equal(t, val.StakingPower, vset.TotalStakingPower())
	assert.NotNil(t, vset.Hash())
	assert.NotPanics(t, func() { vset.IncrementProposerPriority(1) })
	assert.Equal(t, val.Address,
		vset.SelectProposer([]byte{}, 1, 0).Address)

	// update
	val = randValidator(vset.TotalStakingPower())
	assert.NoError(t, vset.UpdateWithChangeSet([]*Validator{val}))
	_, val = vset.GetByAddress(val.Address)
	val.StakingPower += 100
	proposerPriority := val.ProposerPriority

	val.ProposerPriority = 0
	assert.NoError(t, vset.UpdateWithChangeSet([]*Validator{val}))
	_, val = vset.GetByAddress(val.Address)
	assert.Equal(t, proposerPriority, val.ProposerPriority)

}

func TestValidatorSetValidateBasic(t *testing.T) {
	val, _ := RandValidator(false, 1)
	badVal := &Validator{}

	testCases := []struct {
		vals ValidatorSet
		err  bool
		msg  string
	}{
		{
			vals: ValidatorSet{},
			err:  true,
			msg:  "validator set is nil or empty",
		},
		{
			vals: ValidatorSet{
				Validators: []*Validator{},
			},
			err: true,
			msg: "validator set is nil or empty",
		},
		{
			vals: ValidatorSet{
				Validators: []*Validator{badVal},
			},
			err: true,
			msg: "invalid validator #0: validator does not have a public key",
		},
		{
			vals: ValidatorSet{
				Validators: []*Validator{val},
			},
			err: false,
			msg: "",
		},
	}

	for _, tc := range testCases {
		err := tc.vals.ValidateBasic()
		if tc.err {
			if assert.Error(t, err) {
				assert.Equal(t, tc.msg, err.Error())
			}
		} else {
			assert.NoError(t, err)
		}
	}

}

func TestCopy(t *testing.T) {
	vset := randValidatorSet(10)
	vsetHash := vset.Hash()
	if len(vsetHash) == 0 {
		t.Fatalf("ValidatorSet had unexpected zero hash")
	}

	vsetCopy := vset.Copy()
	vsetCopyHash := vsetCopy.Hash()

	if !bytes.Equal(vsetHash, vsetCopyHash) {
		t.Fatalf("ValidatorSet copy had wrong hash. Orig: %X, Copy: %X", vsetHash, vsetCopyHash)
	}
}

// Test that IncrementProposerPriority requires positive times.
func TestIncrementProposerPriorityPositiveTimes(t *testing.T) {
	vset := NewValidatorSet([]*Validator{
		newValidator([]byte("foo"), 1000),
		newValidator([]byte("bar"), 300),
		newValidator([]byte("baz"), 330),
	})

	assert.Panics(t, func() { vset.IncrementProposerPriority(-1) })
	assert.Panics(t, func() { vset.IncrementProposerPriority(0) })
	vset.IncrementProposerPriority(1)
}

func BenchmarkValidatorSetCopy(b *testing.B) {
	b.StopTimer()
	vset := NewValidatorSet([]*Validator{})
	for i := 0; i < 1000; i++ {
		privKey := ed25519.GenPrivKey()
		pubKey := privKey.PubKey()
		val := NewValidator(pubKey, 10)
		err := vset.UpdateWithChangeSet([]*Validator{val})
		if err != nil {
			panic("Failed to add validator")
		}
	}
	b.StartTimer()

	for i := 0; i < b.N; i++ {
		vset.Copy()
	}
}

//-------------------------------------------------------------------

func bytesToInt(b []byte) int {
	i := big.Int{}
	i.SetBytes(b)
	return int(i.Int64())
}

func verifyWinningRate(t *testing.T, vals *ValidatorSet, tries int, error float64) {
	selected := make([]int, len(vals.Validators))
	for i := 0; i < tries; i++ {
		prop := vals.SelectProposer([]byte{}, int64(i), 0)
		for j := 0; j < len(vals.Validators); j++ {
			if bytes.Equal(prop.Address, vals.Validators[j].Address) {
				selected[j]++
				break
			}
		}
	}
	actual := make([]float64, len(vals.Validators))
	for i := 0; i < len(selected); i++ {
		actual[i] = float64(selected[i]) / float64(tries)
	}

	for i := 0; i < len(actual); i++ {
		expected := float64(vals.Validators[i].StakingPower) / float64(vals.TotalStakingPower())
		if math.Abs(expected-actual[i]) > expected*error {
			t.Errorf("The winning rate is too far off from expected: %f ∉ %f±%f",
				actual[i], expected, expected*error)
		}
	}
}

func TestProposerSelection1(t *testing.T) {
	vset := NewValidatorSet([]*Validator{
		newValidator([]byte("foo"), 1000),
		newValidator([]byte("bar"), 300),
		newValidator([]byte("baz"), 330),
	})
	var proposers []string
	for i := 0; i < 99; i++ {
		val := vset.SelectProposer([]byte{}, int64(i), 0)
		proposers = append(proposers, string(val.Address))
	}
	expected := `foo foo foo foo bar bar foo bar foo baz bar foo baz baz baz foo foo bar foo bar baz bar foo baz foo ` +
		`foo baz foo foo baz foo foo baz bar foo foo foo baz foo baz baz bar foo foo foo foo baz bar bar bar bar foo ` +
		`foo foo baz foo foo foo foo foo foo baz foo foo baz bar bar foo bar foo foo baz bar foo foo baz foo foo baz ` +
		`foo foo bar foo foo baz foo foo foo bar foo foo baz baz foo foo bar baz foo baz`

	if expected != strings.Join(proposers, " ") {
		t.Errorf("expected sequence of proposers was\n%v\nbut got \n%v", expected, strings.Join(proposers, " "))
	}
}

func TestProposerSelection2(t *testing.T) {
	addr0 := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	addr1 := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}
	addr2 := []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2}

	// when all voting power is same, the order is random but deterministic
	val0, val1, val2 := newValidator(addr0, 100), newValidator(addr1, 100), newValidator(addr2, 100)
	valList := []*Validator{val0, val1, val2}
	vals := NewValidatorSet(valList)
	expected := []int{0, 1, 0, 0, 2, 2, 0, 2, 1, 2, 2, 1, 2, 2, 2}
	for i := 0; i < len(valList)*5; i++ {
		prop := vals.SelectProposer([]byte{}, int64(i), 0)
		if bytesToInt(prop.Address) != expected[i] {
			t.Fatalf("(%d): Expected %d. Got %d", i, expected[i], bytesToInt(prop.Address))
		}
	}
	verifyWinningRate(t, vals, 10000, 0.01)

	// One validator has more than the others
	*val2 = *newValidator(addr2, 400)
	vals = NewValidatorSet(valList)
	verifyWinningRate(t, vals, 10000, 0.01)

	// One validator has more than the others
	*val2 = *newValidator(addr2, 401)
	vals = NewValidatorSet(valList)
	verifyWinningRate(t, vals, 10000, 0.01)

	// each validator should be the proposer a proportional number of times
	val0, val1, val2 = newValidator(addr0, 4), newValidator(addr1, 5), newValidator(addr2, 3)
	valList = []*Validator{val0, val1, val2}
	propCount := make([]int, 3)
	vals = NewValidatorSet(valList)
	N := 4 + 5 + 3
	for i := 0; i < 10000*N; i++ {
		prop := vals.SelectProposer([]byte{}, int64(i), 0)
		propCount[bytesToInt(prop.Address)]++
	}
	fmt.Printf("%v\n", propCount)

	if propCount[0] != 40257 {
		t.Fatalf(
			"Expected prop count for validator with 4/12 of voting power to be %d/%d. Got %d/%d",
			40038,
			10000*N,
			propCount[0],
			10000*N,
		)
	}
	if propCount[1] != 50017 {
		t.Fatalf(
			"Expected prop count for validator with 5/12 of voting power to be %d/%d. Got %d/%d",
			50077,
			10000*N,
			propCount[1],
			10000*N,
		)
	}
	if propCount[2] != 29726 {
		t.Fatalf(
			"Expected prop count for validator with 3/12 of voting power to be %d/%d. Got %d/%d",
			29885,
			10000*N,
			propCount[2],
			10000*N,
		)
	}
}

func TestProposerSelection3(t *testing.T) {
	vset := NewValidatorSet([]*Validator{
		newValidator([]byte("avalidator_address12"), 1),
		newValidator([]byte("bvalidator_address12"), 1),
		newValidator([]byte("cvalidator_address12"), 1),
		newValidator([]byte("dvalidator_address12"), 1),
	})

	for i := 0; i < len(vset.Validators); i++ {
		// need to give all validators to have keys
		pk := ed25519.GenPrivKey().PubKey()
		vset.Validators[i].PubKey = pk
	}

	proposerOrder := make([]*Validator, 10000)
	for i := 0; i < len(proposerOrder); i++ {
		proposerOrder[i] = vset.SelectProposer([]byte{}, int64(i), 0)
		vset.IncrementProposerPriority(1)
	}

	// i for the loop
	// j for the times
	// we should go in order for ever, despite some IncrementProposerPriority with times > 1
	var (
		i int
	)
	for ; i < len(proposerOrder); i++ {
		got := vset.SelectProposer([]byte{}, int64(i), 0).Address
		expected := proposerOrder[i].Address
		if !bytes.Equal(got, expected) {
			t.Fatalf(fmt.Sprintf("vset.Proposer (%X) does not match expected proposer (%X) for %d", got, expected, i))
		}

		// serialize, deserialize, check proposer
		b := vset.toBytes()
		vset = vset.fromBytes(b)

		computed := vset.SelectProposer([]byte{}, int64(i), 0) // findGetProposer()
		if i != 0 {
			if !bytes.Equal(got, computed.Address) {
				t.Fatalf(
					fmt.Sprintf(
						"vset.Proposer (%X) does not match computed proposer (%X) for %d",
						got,
						computed.Address,
						i,
					),
				)
			}
		}
		vset.IncrementProposerPriority(1)
	}
}

func newValidator(address []byte, power int64) *Validator {
	return &Validator{Address: address, StakingPower: power, PubKey: randPubKey()}
}

func randPubKey() crypto.PubKey {
	pubKey := make(ed25519.PubKey, ed25519.PubKeySize)
	copy(pubKey, tmrand.Bytes(32))
	return ed25519.PubKey(tmrand.Bytes(32))
}

func defendLimit(a int64) int64 {
	if a <= 0 {
		return 1
	}
	if a > MaxTotalStakingPower/8 {
		a = MaxTotalStakingPower / 8
	}
	return a
}

func randValidator(totalStakingPower int64) *Validator {
	// this modulo limits the ProposerPriority/StakingPower to stay in the
	// bounds of MaxTotalStakingPower minus the already existing voting power:
	stakingPower := defendLimit(int64(tmrand.Uint64() % uint64(MaxTotalStakingPower-totalStakingPower)))
	val := NewValidator(randPubKey(), stakingPower)
	val.ProposerPriority = stakingPower
	return val
}

func randValidatorSet(numValidators int) *ValidatorSet {
	validators := make([]*Validator, numValidators)
	totalStakingPower := int64(numValidators) // to depend for total staking power to be over MaxTotalStakingPower
	for i := 0; i < numValidators; i++ {
		validators[i] = randValidator(totalStakingPower)
		totalStakingPower += validators[i].StakingPower
		if totalStakingPower >= MaxTotalStakingPower {
			// the remainder must have 1 of staking power
			totalStakingPower = MaxTotalStakingPower - 1
		}
	}
	return NewValidatorSet(validators)
}

func randValidatorWithMinMax(keyType PvKeyType, min, max int64) (*Validator, PrivValidator) {
	privVal := NewMockPV(keyType)
	pubKey, _ := privVal.GetPubKey()
	val := NewValidator(pubKey, min+int64(tmrand.Uint64()%uint64(1+max-min)))
	val.ProposerPriority = min + tmrand.Int64()%max
	return val, privVal
}

func randValidatorSetWithMinMax(keyType PvKeyType, numValidators int, min, max int64) (*ValidatorSet,
	map[string]PrivValidator) {
	validators := make([]*Validator, numValidators)
	privMap := make(map[string]PrivValidator)
	var privVal PrivValidator
	for i := 0; i < numValidators; i++ {
		validators[i], privVal = randValidatorWithMinMax(keyType, min, max)
		privMap[validators[i].Address.String()] = privVal
	}
	return NewValidatorSet(validators), privMap
}

func (vals *ValidatorSet) toBytes() []byte {
	pbvs, err := vals.ToProto()
	if err != nil {
		panic(err)
	}

	bz, err := pbvs.Marshal()
	if err != nil {
		panic(err)
	}

	return bz
}

func (vals *ValidatorSet) fromBytes(b []byte) *ValidatorSet {
	pbvs := new(tmproto.ValidatorSet)
	err := pbvs.Unmarshal(b)
	if err != nil {
		// DATA HAS BEEN CORRUPTED OR THE SPEC HAS CHANGED
		panic(err)
	}

	vs, err := ValidatorSetFromProto(pbvs)
	if err != nil {
		panic(err)
	}

	return vs
}

//-------------------------------------------------------------------

func TestValidatorSetTotalStakingPowerPanicsOnOverflow(t *testing.T) {
	// NewValidatorSet calls IncrementProposerPriority which calls TotalStakingPower()
	// which should panic on overflows:
	shouldPanic := func() {
		NewValidatorSet([]*Validator{
			{Address: []byte("a"), StakingPower: math.MaxInt64, ProposerPriority: 0},
			{Address: []byte("b"), StakingPower: math.MaxInt64, ProposerPriority: 0},
			{Address: []byte("c"), StakingPower: math.MaxInt64, ProposerPriority: 0},
		})
	}

	assert.Panics(t, shouldPanic)
}

func TestAvgProposerPriority(t *testing.T) {
	// Create Validator set without calling IncrementProposerPriority:
	tcs := []struct {
		vs   ValidatorSet
		want int64
	}{
		0: {ValidatorSet{Validators: []*Validator{{ProposerPriority: 0}, {ProposerPriority: 0}, {ProposerPriority: 0}}}, 0},
		1: {
			ValidatorSet{
				Validators: []*Validator{{ProposerPriority: math.MaxInt64}, {ProposerPriority: 0}, {ProposerPriority: 0}},
			}, math.MaxInt64 / 3,
		},
		2: {
			ValidatorSet{
				Validators: []*Validator{{ProposerPriority: math.MaxInt64}, {ProposerPriority: 0}},
			}, math.MaxInt64 / 2,
		},
		3: {
			ValidatorSet{
				Validators: []*Validator{{ProposerPriority: math.MaxInt64}, {ProposerPriority: math.MaxInt64}},
			}, math.MaxInt64,
		},
		4: {
			ValidatorSet{
				Validators: []*Validator{{ProposerPriority: math.MinInt64}, {ProposerPriority: math.MinInt64}},
			}, math.MinInt64,
		},
	}
	for i, tc := range tcs {
		got := tc.vs.computeAvgProposerPriority()
		assert.Equal(t, tc.want, got, "test case: %v", i)
	}
}

func TestAveragingInIncrementProposerPriority(t *testing.T) {
	// FIXME
	t.Skip("The ProposerPriority validated in this test case is excluded because it is not used in VRF.")

	// Test that the averaging works as expected inside of IncrementProposerPriority.
	// Each validator comes with zero voting power which simplifies reasoning about
	// the expected ProposerPriority.
	tcs := []struct {
		vs    ValidatorSet
		times int32
		avg   int64
	}{
		0: {ValidatorSet{
			Validators: []*Validator{
				{Address: []byte("a"), ProposerPriority: 1},
				{Address: []byte("b"), ProposerPriority: 2},
				{Address: []byte("c"), ProposerPriority: 3}}},
			1, 2},
		1: {ValidatorSet{
			Validators: []*Validator{
				{Address: []byte("a"), ProposerPriority: 10},
				{Address: []byte("b"), ProposerPriority: -10},
				{Address: []byte("c"), ProposerPriority: 1}}},
			// this should average twice but the average should be 0 after the first iteration
			// (voting power is 0 -> no changes)
			11, 1 / 3},
		2: {ValidatorSet{
			Validators: []*Validator{
				{Address: []byte("a"), ProposerPriority: 100},
				{Address: []byte("b"), ProposerPriority: -10},
				{Address: []byte("c"), ProposerPriority: 1}}},
			1, 91 / 3},
	}
	for i, tc := range tcs {
		// work on copy to have the old ProposerPriorities:
		newVset := tc.vs.Copy()
		for _, val := range tc.vs.Validators {
			_, updatedVal := newVset.GetByAddress(val.Address)
			assert.Equal(t, updatedVal.ProposerPriority, val.ProposerPriority-tc.avg, "test case: %v", i)
		}
	}
}

func TestAveragingInIncrementProposerPriorityWithStakingPower(t *testing.T) {
	// Other than TestAveragingInIncrementProposerPriority this is a more complete test showing
	// how each ProposerPriority changes in relation to the validator's voting power respectively.
	// average is zero in each round:
	vp0 := int64(10)
	vp1 := int64(1)
	vp2 := int64(1)
	total := vp0 + vp1 + vp2
	avg := (vp0 + vp1 + vp2 - total) / 3
	vals := ValidatorSet{Validators: []*Validator{
		{Address: []byte{0}, ProposerPriority: 0, StakingPower: vp0},
		{Address: []byte{1}, ProposerPriority: 0, StakingPower: vp1},
		{Address: []byte{2}, ProposerPriority: 0, StakingPower: vp2}}}
	tcs := []struct {
		vals                  *ValidatorSet
		wantProposerPrioritys []int64
		times                 int32
		wantProposer          *Validator
	}{

		0: {
			vals.Copy(),
			[]int64{
				// Acumm+StakingPower-Avg:
				0 + vp0 - total - avg, // mostest will be subtracted by total voting power (12)
				0 + vp1,
				0 + vp2},
			1,
			vals.Validators[0]},
		1: {
			vals.Copy(),
			[]int64{
				(0 + vp0 - total) + vp0 - total - avg, // this will be mostest on 2nd iter, too
				(0 + vp1) + vp1,
				(0 + vp2) + vp2},
			2,
			vals.Validators[0]}, // increment twice -> expect average to be subtracted twice
		2: {
			vals.Copy(),
			[]int64{
				0 + 3*(vp0-total) - avg, // still mostest
				0 + 3*vp1,
				0 + 3*vp2},
			3,
			vals.Validators[0]},
		3: {
			vals.Copy(),
			[]int64{
				0 + 4*(vp0-total), // still mostest
				0 + 4*vp1,
				0 + 4*vp2},
			4,
			vals.Validators[0]},
		4: {
			vals.Copy(),
			[]int64{
				0 + 4*(vp0-total) + vp0, // 4 iters was mostest
				0 + 5*vp1 - total,       // now this val is mostest for the 1st time (hence -12==totalStakingPower)
				0 + 5*vp2},
			5,
			vals.Validators[2]},
		5: {
			vals.Copy(),
			[]int64{
				0 + 6*vp0 - 5*total, // mostest again
				0 + 6*vp1 - total,   // mostest once up to here
				0 + 6*vp2},
			6,
			vals.Validators[2]},
		6: {
			vals.Copy(),
			[]int64{
				0 + 7*vp0 - 6*total, // in 7 iters this val is mostest 6 times
				0 + 7*vp1 - total,   // in 7 iters this val is mostest 1 time
				0 + 7*vp2},
			7,
			vals.Validators[0]},
		7: {
			vals.Copy(),
			[]int64{
				0 + 8*vp0 - 7*total, // mostest again
				0 + 8*vp1 - total,
				0 + 8*vp2},
			8,
			vals.Validators[2]},
		8: {
			vals.Copy(),
			[]int64{
				0 + 9*vp0 - 7*total,
				0 + 9*vp1 - total,
				0 + 9*vp2 - total}, // mostest
			9,
			vals.Validators[0]},
		9: {
			vals.Copy(),
			[]int64{
				0 + 10*vp0 - 8*total, // after 10 iters this is mostest again
				0 + 10*vp1 - total,   // after 6 iters this val is "mostest" once and not in between
				0 + 10*vp2 - total},  // in between 10 iters this val is "mostest" once
			10,
			vals.Validators[0]},
		10: {
			vals.Copy(),
			[]int64{
				0 + 11*vp0 - 9*total,
				0 + 11*vp1 - total,  // after 6 iters this val is "mostest" once and not in between
				0 + 11*vp2 - total}, // after 10 iters this val is "mostest" once
			11,
			vals.Validators[1]},
	}
	for i, tc := range tcs {
		tc.vals.IncrementProposerPriority(tc.times)

		assert.Equal(t, tc.wantProposer.Address,
			tc.vals.SelectProposer([]byte{}, int64(i), 0).Address,
			"test case: %v",
			i)
		for valIdx, val := range tc.vals.Validators {
			assert.Equal(t,
				tc.wantProposerPrioritys[valIdx],
				val.ProposerPriority,
				"test case: %v, validator: %v",
				i,
				valIdx)
		}
	}
}

func TestSafeAdd(t *testing.T) {
	f := func(a, b int64) bool {
		c, overflow := safeAdd(a, b)
		return overflow || (!overflow && c == a+b)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func TestSafeAddClip(t *testing.T) {
	assert.EqualValues(t, math.MaxInt64, safeAddClip(math.MaxInt64, 10))
	assert.EqualValues(t, math.MaxInt64, safeAddClip(math.MaxInt64, math.MaxInt64))
	assert.EqualValues(t, math.MinInt64, safeAddClip(math.MinInt64, -10))
}

func TestSafeSubClip(t *testing.T) {
	assert.EqualValues(t, math.MinInt64, safeSubClip(math.MinInt64, 10))
	assert.EqualValues(t, 0, safeSubClip(math.MinInt64, math.MinInt64))
	assert.EqualValues(t, math.MinInt64, safeSubClip(math.MinInt64, math.MaxInt64))
	assert.EqualValues(t, math.MaxInt64, safeSubClip(math.MaxInt64, -10))
}

//-------------------------------------------------------------------

func TestValidatorSetVerifyCommit(t *testing.T) {
	privKey := ed25519.GenPrivKey()
	pubKey := privKey.PubKey()
	v1 := NewValidator(pubKey, 1000)
	vset := ToVoterAll([]*Validator{v1})

	// good
	var (
		chainID = "mychainID"
		blockID = makeBlockIDRandom()
		height  = int64(5)
	)
	vote := &Vote{
		ValidatorAddress: v1.Address,
		ValidatorIndex:   0,
		Height:           height,
		Round:            0,
		Timestamp:        tmtime.Now(),
		Type:             tmproto.PrecommitType,
		BlockID:          blockID,
	}
	sig, err := privKey.Sign(VoteSignBytes(chainID, vote.ToProto()))
	assert.NoError(t, err)
	vote.Signature = sig
	commit := NewCommit(vote.Height, vote.Round, blockID, []CommitSig{vote.CommitSig()})

	// bad
	var (
		badChainID = "notmychainID"
		badBlockID = BlockID{Hash: []byte("goodbye")}
		badHeight  = height + 1
		badCommit  = NewCommit(badHeight, 0, blockID, []CommitSig{{BlockIDFlag: BlockIDFlagAbsent}})
	)

	// test some error cases
	// TODO: test more cases!
	cases := []struct {
		chainID string
		blockID BlockID
		height  int64
		commit  *Commit
	}{
		{badChainID, blockID, height, commit},
		{chainID, badBlockID, height, commit},
		{chainID, blockID, badHeight, commit},
		{chainID, blockID, height, badCommit},
	}

	for i, c := range cases {
		err := vset.VerifyCommit(c.chainID, c.blockID, c.height, c.commit)
		assert.NotNil(t, err, i)
	}

	// test a good one
	err = vset.VerifyCommit(chainID, blockID, height, commit)
	assert.Nil(t, err)
}

func TestEmptySet(t *testing.T) {

	var valList []*Validator
	valSet := NewValidatorSet(valList)
	assert.Panics(t, func() { valSet.IncrementProposerPriority(1) })
	assert.Panics(t, func() { valSet.RescalePriorities(100) })
	assert.Panics(t, func() { valSet.shiftByAvgProposerPriority() })
	assert.Panics(t, func() { assert.Zero(t, computeMaxMinPriorityDiff(valSet)) })

	// Add to empty set
	v1 := newValidator([]byte("v1"), 100)
	v2 := newValidator([]byte("v2"), 100)
	valList = []*Validator{v1, v2}
	assert.NoError(t, valSet.UpdateWithChangeSet(valList))
	verifyValidatorSet(t, valSet)

	// Delete all validators from set
	v1 = newValidator([]byte("v1"), 0)
	v2 = newValidator([]byte("v2"), 0)
	delList := []*Validator{v1, v2}
	assert.Error(t, valSet.UpdateWithChangeSet(delList))

	// Attempt delete from empty set
	assert.Error(t, valSet.UpdateWithChangeSet(delList))

}

func TestUpdatesForNewValidatorSet(t *testing.T) {
	v1 := newValidator([]byte("v1"), 100)
	v2 := newValidator([]byte("v2"), 100)
	valList := []*Validator{v1, v2}
	valSet := NewValidatorSet(valList)
	verifyValidatorSet(t, valSet)

	// Verify duplicates are caught in NewValidatorSet() and it panics
	v111 := newValidator([]byte("v1"), 100)
	v112 := newValidator([]byte("v1"), 123)
	v113 := newValidator([]byte("v1"), 234)
	valList = []*Validator{v111, v112, v113}
	assert.Panics(t, func() { NewValidatorSet(valList) })

	// Verify set including validator with voting power 0 cannot be created
	v1 = newValidator([]byte("v1"), 0)
	v2 = newValidator([]byte("v2"), 22)
	v3 := newValidator([]byte("v3"), 33)
	valList = []*Validator{v1, v2, v3}
	assert.Panics(t, func() { NewValidatorSet(valList) })

	// Verify set including validator with negative voting power cannot be created
	v1 = newValidator([]byte("v1"), 10)
	v2 = newValidator([]byte("v2"), -20)
	v3 = newValidator([]byte("v3"), 30)
	valList = []*Validator{v1, v2, v3}
	assert.Panics(t, func() { NewValidatorSet(valList) })

}

type testVal struct {
	name  string
	power int64
}

func permutation(valList []testVal) []testVal {
	if len(valList) == 0 {
		return nil
	}
	permList := make([]testVal, len(valList))
	perm := tmrand.Perm(len(valList))
	for i, v := range perm {
		permList[v] = valList[i]
	}
	return permList
}

func createNewValidatorList(testValList []testVal) []*Validator {
	valList := make([]*Validator, 0, len(testValList))
	for _, val := range testValList {
		valList = append(valList, newValidator([]byte(val.name), val.power))
	}
	return valList
}

func createNewValidatorSet(testValList []testVal) *ValidatorSet {
	return NewValidatorSet(createNewValidatorList(testValList))
}

func valSetTotalProposerPriority(valSet *ValidatorSet) int64 {
	sum := int64(0)
	for _, val := range valSet.Validators {
		// mind overflow
		sum = safeAddClip(sum, val.ProposerPriority)
	}
	return sum
}

func verifyValidatorSet(t *testing.T, valSet *ValidatorSet) {
	// verify that the capacity and length of validators is the same
	assert.Equal(t, len(valSet.Validators), cap(valSet.Validators))

	// verify that the set's total voting power has been updated
	tvp := valSet.totalStakingPower
	valSet.updateTotalStakingPower()
	expectedTvp := valSet.TotalStakingPower()
	assert.Equal(t, expectedTvp, tvp,
		"expected TVP %d. Got %d, voterSet=%s", expectedTvp, tvp, valSet)

	// verify that validator priorities are centered
	valsCount := int64(len(valSet.Validators))
	tpp := valSetTotalProposerPriority(valSet)
	assert.True(t, tpp < valsCount && tpp > -valsCount,
		"expected total priority in (-%d, %d). Got %d", valsCount, valsCount, tpp)

	// verify that priorities are scaled
	dist := computeMaxMinPriorityDiff(valSet)
	assert.True(t, dist <= PriorityWindowSizeFactor*tvp,
		"expected priority distance < %d. Got %d", PriorityWindowSizeFactor*tvp, dist)
}

func toTestValList(valList []*Validator) []testVal {
	testList := make([]testVal, len(valList))
	for i, val := range valList {
		testList[i].name = string(val.Address)
		testList[i].power = val.StakingPower
	}
	return testList
}

func testValSet(nVals int, power int64) []testVal {
	vals := make([]testVal, nVals)
	for i := 0; i < nVals; i++ {
		vals[i] = testVal{fmt.Sprintf("v%d", i+1), power}
	}
	return vals
}

type valSetErrTestCase struct {
	startVals  []testVal
	updateVals []testVal
}

func executeValSetErrTestCase(t *testing.T, idx int, tt valSetErrTestCase) {
	// create a new set and apply updates, keeping copies for the checks
	valSet := createNewValidatorSet(tt.startVals)
	valSetCopy := valSet.Copy()
	valList := createNewValidatorList(tt.updateVals)
	valListCopy := validatorListCopy(valList)
	err := valSet.UpdateWithChangeSet(valList)

	// for errors check the validator set has not been changed
	assert.Error(t, err, "test %d", idx)
	assert.Equal(t, valSet, valSetCopy, "test %v", idx)

	// check the parameter list has not changed
	assert.Equal(t, valList, valListCopy, "test %v", idx)
}

func TestValSetUpdatesDuplicateEntries(t *testing.T) {
	testCases := []valSetErrTestCase{
		// Duplicate entries in changes
		{ // first entry is duplicated change
			testValSet(2, 10),
			[]testVal{{"v1", 11}, {"v1", 22}},
		},
		{ // second entry is duplicated change
			testValSet(2, 10),
			[]testVal{{"v2", 11}, {"v2", 22}},
		},
		{ // change duplicates are separated by a valid change
			testValSet(2, 10),
			[]testVal{{"v1", 11}, {"v2", 22}, {"v1", 12}},
		},
		{ // change duplicates are separated by a valid change
			testValSet(3, 10),
			[]testVal{{"v1", 11}, {"v3", 22}, {"v1", 12}},
		},

		// Duplicate entries in remove
		{ // first entry is duplicated remove
			testValSet(2, 10),
			[]testVal{{"v1", 0}, {"v1", 0}},
		},
		{ // second entry is duplicated remove
			testValSet(2, 10),
			[]testVal{{"v2", 0}, {"v2", 0}},
		},
		{ // remove duplicates are separated by a valid remove
			testValSet(2, 10),
			[]testVal{{"v1", 0}, {"v2", 0}, {"v1", 0}},
		},
		{ // remove duplicates are separated by a valid remove
			testValSet(3, 10),
			[]testVal{{"v1", 0}, {"v3", 0}, {"v1", 0}},
		},

		{ // remove and update same val
			testValSet(2, 10),
			[]testVal{{"v1", 0}, {"v2", 20}, {"v1", 30}},
		},
		{ // duplicate entries in removes + changes
			testValSet(2, 10),
			[]testVal{{"v1", 0}, {"v2", 20}, {"v2", 30}, {"v1", 0}},
		},
		{ // duplicate entries in removes + changes
			testValSet(3, 10),
			[]testVal{{"v1", 0}, {"v3", 5}, {"v2", 20}, {"v2", 30}, {"v1", 0}},
		},
	}

	for i, tt := range testCases {
		executeValSetErrTestCase(t, i, tt)
	}
}

func TestValSetUpdatesOverflows(t *testing.T) {
	maxVP := MaxTotalStakingPower
	testCases := []valSetErrTestCase{
		{ // single update leading to overflow
			testValSet(2, 10),
			[]testVal{{"v1", math.MaxInt64}},
		},
		{ // single update leading to overflow
			testValSet(2, 10),
			[]testVal{{"v2", math.MaxInt64}},
		},
		{ // add validator leading to overflow
			testValSet(1, maxVP),
			[]testVal{{"v2", math.MaxInt64}},
		},
		{ // add validator leading to exceed Max
			testValSet(1, maxVP-1),
			[]testVal{{"v2", 5}},
		},
		{ // add validator leading to exceed Max
			testValSet(2, maxVP/3),
			[]testVal{{"v3", maxVP / 2}},
		},
		{ // add validator leading to exceed Max
			testValSet(1, maxVP),
			[]testVal{{"v2", maxVP}},
		},
	}

	for i, tt := range testCases {
		executeValSetErrTestCase(t, i, tt)
	}
}

func TestValSetUpdatesOtherErrors(t *testing.T) {
	testCases := []valSetErrTestCase{
		{ // update with negative voting power
			testValSet(2, 10),
			[]testVal{{"v1", -123}},
		},
		{ // update with negative voting power
			testValSet(2, 10),
			[]testVal{{"v2", -123}},
		},
		{ // remove non-existing validator
			testValSet(2, 10),
			[]testVal{{"v3", 0}},
		},
		{ // delete all validators
			[]testVal{{"v1", 10}, {"v2", 20}, {"v3", 30}},
			[]testVal{{"v1", 0}, {"v2", 0}, {"v3", 0}},
		},
	}

	for i, tt := range testCases {
		executeValSetErrTestCase(t, i, tt)
	}
}

func TestValSetUpdatesBasicTestsExecute(t *testing.T) {
	valSetUpdatesBasicTests := []struct {
		startVals    []testVal
		updateVals   []testVal
		expectedVals []testVal
	}{
		{ // no changes
			testValSet(2, 10),
			[]testVal{},
			testValSet(2, 10),
		},
		{ // voting power changes
			testValSet(2, 10),
			[]testVal{{"v2", 22}, {"v1", 11}},
			[]testVal{{"v2", 22}, {"v1", 11}},
		},
		{ // add new validators
			[]testVal{{"v2", 20}, {"v1", 10}},
			[]testVal{{"v4", 40}, {"v3", 30}},
			[]testVal{{"v4", 40}, {"v3", 30}, {"v2", 20}, {"v1", 10}},
		},
		{ // add new validator to middle
			[]testVal{{"v3", 20}, {"v1", 10}},
			[]testVal{{"v2", 30}},
			[]testVal{{"v2", 30}, {"v3", 20}, {"v1", 10}},
		},
		{ // add new validator to beginning
			[]testVal{{"v3", 20}, {"v2", 10}},
			[]testVal{{"v1", 30}},
			[]testVal{{"v1", 30}, {"v3", 20}, {"v2", 10}},
		},
		{ // delete validators
			[]testVal{{"v3", 30}, {"v2", 20}, {"v1", 10}},
			[]testVal{{"v2", 0}},
			[]testVal{{"v3", 30}, {"v1", 10}},
		},
	}

	for i, tt := range valSetUpdatesBasicTests {
		// create a new set and apply updates, keeping copies for the checks
		valSet := createNewValidatorSet(tt.startVals)
		valList := createNewValidatorList(tt.updateVals)
		err := valSet.UpdateWithChangeSet(valList)
		assert.NoError(t, err, "test %d", i)

		valListCopy := validatorListCopy(valSet.Validators)
		// check that the voting power in the set's validators is not changing if the voting power
		// is changed in the list of validators previously passed as parameter to UpdateWithChangeSet.
		// this is to make sure copies of the validators are made by UpdateWithChangeSet.
		if len(valList) > 0 {
			valList[0].StakingPower++
			assert.Equal(t, toTestValList(valListCopy), toTestValList(valSet.Validators), "test %v", i)

		}

		// check the final validator list is as expected and the set is properly scaled and centered.
		assert.Equal(t, tt.expectedVals, toTestValList(valSet.Validators), "test %v", i)
		verifyValidatorSet(t, valSet)
	}
}

// Test that different permutations of an update give the same result.
func TestValSetUpdatesOrderIndependenceTestsExecute(t *testing.T) {
	// FIXME
	t.Skip("Temporarily excluded because this a case that doesn't end due to Proposer selection changes.")

	// startVals - initial validators to create the set with
	// updateVals - a sequence of updates to be applied to the set.
	// updateVals is shuffled a number of times during testing to check for same resulting validator set.
	valSetUpdatesOrderTests := []struct {
		startVals  []testVal
		updateVals []testVal
	}{
		0: { // order of changes should not matter, the final validator sets should be the same
			[]testVal{{"v4", 40}, {"v3", 30}, {"v2", 10}, {"v1", 10}},
			[]testVal{{"v4", 44}, {"v3", 33}, {"v2", 22}, {"v1", 11}}},

		1: { // order of additions should not matter
			[]testVal{{"v2", 20}, {"v1", 10}},
			[]testVal{{"v3", 30}, {"v4", 40}, {"v5", 50}, {"v6", 60}}},

		2: { // order of removals should not matter
			[]testVal{{"v4", 40}, {"v3", 30}, {"v2", 20}, {"v1", 10}},
			[]testVal{{"v1", 0}, {"v3", 0}, {"v4", 0}}},

		3: { // order of mixed operations should not matter
			[]testVal{{"v4", 40}, {"v3", 30}, {"v2", 20}, {"v1", 10}},
			[]testVal{{"v1", 0}, {"v3", 0}, {"v2", 22}, {"v5", 50}, {"v4", 44}}},
	}

	for i, tt := range valSetUpdatesOrderTests {
		// create a new set and apply updates
		valSet := createNewValidatorSet(tt.startVals)
		valSetCopy := valSet.Copy()
		valList := createNewValidatorList(tt.updateVals)
		assert.NoError(t, valSetCopy.UpdateWithChangeSet(valList))

		// save the result as expected for next updates
		valSetExp := valSetCopy.Copy()

		// perform at most 20 permutations on the updates and call UpdateWithChangeSet()
		n := len(tt.updateVals)
		maxNumPerms := tmmath.MinInt(20, n*n)
		for j := 0; j < maxNumPerms; j++ {
			// create a copy of original set and apply a random permutation of updates
			valSetCopy := valSet.Copy()
			valList := createNewValidatorList(permutation(tt.updateVals))

			// check there was no error and the set is properly scaled and centered.
			assert.NoError(t, valSetCopy.UpdateWithChangeSet(valList),
				"test %v failed for permutation %v", i, valList)
			verifyValidatorSet(t, valSetCopy)

			// verify the resulting test is same as the expected
			assert.Equal(t, valSetCopy, valSetExp,
				"test %v failed for permutation %v", i, valList)
		}
	}
}

// This tests the private function validator_set.go:applyUpdates() function, used only for additions and changes.
// Should perform a proper merge of updatedVals and startVals
func TestValSetApplyUpdatesTestsExecute(t *testing.T) {
	valSetUpdatesBasicTests := []struct {
		startVals    []testVal
		updateVals   []testVal
		expectedVals []testVal
	}{
		// additions
		0: { // prepend
			[]testVal{{"v4", 44}, {"v5", 55}},
			[]testVal{{"v1", 11}},
			[]testVal{{"v1", 11}, {"v4", 44}, {"v5", 55}}},
		1: { // append
			[]testVal{{"v4", 44}, {"v5", 55}},
			[]testVal{{"v6", 66}},
			[]testVal{{"v4", 44}, {"v5", 55}, {"v6", 66}}},
		2: { // insert
			[]testVal{{"v4", 44}, {"v6", 66}},
			[]testVal{{"v5", 55}},
			[]testVal{{"v4", 44}, {"v5", 55}, {"v6", 66}}},
		3: { // insert multi
			[]testVal{{"v4", 44}, {"v6", 66}, {"v9", 99}},
			[]testVal{{"v5", 55}, {"v7", 77}, {"v8", 88}},
			[]testVal{{"v4", 44}, {"v5", 55}, {"v6", 66}, {"v7", 77}, {"v8", 88}, {"v9", 99}}},
		// changes
		4: { // head
			[]testVal{{"v1", 111}, {"v2", 22}},
			[]testVal{{"v1", 11}},
			[]testVal{{"v1", 11}, {"v2", 22}}},
		5: { // tail
			[]testVal{{"v1", 11}, {"v2", 222}},
			[]testVal{{"v2", 22}},
			[]testVal{{"v1", 11}, {"v2", 22}}},
		6: { // middle
			[]testVal{{"v1", 11}, {"v2", 222}, {"v3", 33}},
			[]testVal{{"v2", 22}},
			[]testVal{{"v1", 11}, {"v2", 22}, {"v3", 33}}},
		7: { // multi
			[]testVal{{"v1", 111}, {"v2", 222}, {"v3", 333}},
			[]testVal{{"v1", 11}, {"v2", 22}, {"v3", 33}},
			[]testVal{{"v1", 11}, {"v2", 22}, {"v3", 33}}},
		// additions and changes
		8: {
			[]testVal{{"v1", 111}, {"v2", 22}},
			[]testVal{{"v1", 11}, {"v3", 33}, {"v4", 44}},
			[]testVal{{"v1", 11}, {"v2", 22}, {"v3", 33}, {"v4", 44}}},
	}

	for i, tt := range valSetUpdatesBasicTests {
		// create a new validator set with the start values
		valSet := createNewValidatorSet(tt.startVals)

		// applyUpdates() with the update values
		valList := createNewValidatorList(tt.updateVals)
		valSet.applyUpdates(valList)

		// check the new list of validators for proper merge
		assert.Equal(t, toTestValList(valSet.Validators), tt.expectedVals, "test %v", i)
	}
}

type testVSetCfg struct {
	name         string
	startVals    []testVal
	deletedVals  []testVal
	updatedVals  []testVal
	addedVals    []testVal
	expectedVals []testVal
	expErr       error
}

func randTestVSetCfg(t *testing.T, nBase, nAddMax int) testVSetCfg {
	if nBase <= 0 || nAddMax < 0 {
		panic(fmt.Sprintf("bad parameters %v %v", nBase, nAddMax))
	}

	const maxPower = 1000
	var nOld, nDel, nChanged, nAdd int

	nOld = int(tmrand.Uint()%uint(nBase)) + 1
	if nBase-nOld > 0 {
		nDel = int(tmrand.Uint() % uint(nBase-nOld))
	}
	nChanged = nBase - nOld - nDel

	if nAddMax > 0 {
		nAdd = tmrand.Int()%nAddMax + 1
	}

	cfg := testVSetCfg{}

	cfg.startVals = make([]testVal, nBase)
	cfg.deletedVals = make([]testVal, nDel)
	cfg.addedVals = make([]testVal, nAdd)
	cfg.updatedVals = make([]testVal, nChanged)
	cfg.expectedVals = make([]testVal, nBase-nDel+nAdd)

	for i := 0; i < nBase; i++ {
		cfg.startVals[i] = testVal{fmt.Sprintf("v%d", i), int64(tmrand.Uint()%maxPower + 1)}
		if i < nOld {
			cfg.expectedVals[i] = cfg.startVals[i]
		}
		if i >= nOld && i < nOld+nChanged {
			cfg.updatedVals[i-nOld] = testVal{fmt.Sprintf("v%d", i), int64(tmrand.Uint()%maxPower + 1)}
			cfg.expectedVals[i] = cfg.updatedVals[i-nOld]
		}
		if i >= nOld+nChanged {
			cfg.deletedVals[i-nOld-nChanged] = testVal{fmt.Sprintf("v%d", i), 0}
		}
	}

	for i := nBase; i < nBase+nAdd; i++ {
		cfg.addedVals[i-nBase] = testVal{fmt.Sprintf("v%d", i), int64(tmrand.Uint()%maxPower + 1)}
		cfg.expectedVals[i-nDel] = cfg.addedVals[i-nBase]
	}

	sort.Sort(testValsByVotingPower(cfg.startVals))
	sort.Sort(testValsByVotingPower(cfg.deletedVals))
	sort.Sort(testValsByVotingPower(cfg.updatedVals))
	sort.Sort(testValsByVotingPower(cfg.addedVals))
	sort.Sort(testValsByVotingPower(cfg.expectedVals))

	return cfg

}

func applyChangesToValSet(t *testing.T, expErr error, valSet *ValidatorSet, valsLists ...[]testVal) {
	changes := make([]testVal, 0)
	for _, valsList := range valsLists {
		changes = append(changes, valsList...)
	}
	valList := createNewValidatorList(changes)
	err := valSet.UpdateWithChangeSet(valList)
	if expErr != nil {
		assert.Equal(t, expErr, err)
	} else {
		assert.NoError(t, err)
	}
}

func TestValSetUpdatePriorityOrderTests(t *testing.T) {
	const nMaxElections = 5000

	testCases := []testVSetCfg{
		0: { // remove high power validator, keep old equal lower power validators
			startVals:    []testVal{{"v3", 1000}, {"v1", 1}, {"v2", 1}},
			deletedVals:  []testVal{{"v3", 0}},
			updatedVals:  []testVal{},
			addedVals:    []testVal{},
			expectedVals: []testVal{{"v1", 1}, {"v2", 1}},
		},
		1: { // remove high power validator, keep old different power validators
			startVals:    []testVal{{"v3", 1000}, {"v2", 10}, {"v1", 1}},
			deletedVals:  []testVal{{"v3", 0}},
			updatedVals:  []testVal{},
			addedVals:    []testVal{},
			expectedVals: []testVal{{"v2", 10}, {"v1", 1}},
		},
		2: { // remove high power validator, add new low power validators, keep old lower power
			startVals:    []testVal{{"v3", 1000}, {"v2", 2}, {"v1", 1}},
			deletedVals:  []testVal{{"v3", 0}},
			updatedVals:  []testVal{{"v2", 1}},
			addedVals:    []testVal{{"v5", 50}, {"v4", 40}},
			expectedVals: []testVal{{"v5", 50}, {"v4", 40}, {"v1", 1}, {"v2", 1}},
		},

		// generate a configuration with 100 validators,
		// randomly select validators for updates and deletes, and
		// generate 10 new validators to be added
		3: randTestVSetCfg(t, 100, 10),

		4: randTestVSetCfg(t, 1000, 100),

		5: randTestVSetCfg(t, 10, 100),

		6: randTestVSetCfg(t, 100, 1000),

		7: randTestVSetCfg(t, 1000, 1000),
	}

	for _, cfg := range testCases {

		// create a new validator set
		valSet := createNewValidatorSet(cfg.startVals)
		verifyValidatorSet(t, valSet)

		// run election up to nMaxElections times, apply changes and verify that the priority order is correct
		verifyValSetUpdatePriorityOrder(t, valSet, cfg, nMaxElections)
	}
}

func verifyValSetUpdatePriorityOrder(t *testing.T, valSet *ValidatorSet, cfg testVSetCfg, nMaxElections int32) {
	// Run election up to nMaxElections times, sort validators by priorities
	valSet.IncrementProposerPriority(tmrand.Int31()%nMaxElections + 1)

	// apply the changes, get the updated validators, sort by priorities
	applyChangesToValSet(t, nil, valSet, cfg.addedVals, cfg.updatedVals, cfg.deletedVals)

	// basic checks
	assert.Equal(t, cfg.expectedVals, toTestValList(valSet.Validators))
	verifyValidatorSet(t, valSet)

	// verify that the added validators have the smallest priority:
	//  - they should be at the beginning of updatedValsPriSorted since it is
	//  sorted by priority
	if len(cfg.addedVals) > 0 {
		updatedValsPriSorted := validatorListCopy(valSet.Validators)
		sort.Sort(validatorsByPriority(updatedValsPriSorted))

		addedValsPriSlice := updatedValsPriSorted[:len(cfg.addedVals)]
		sort.Sort(ValidatorsByVotingPower(addedValsPriSlice))
		assert.Equal(t, cfg.addedVals, toTestValList(addedValsPriSlice))

		//  - and should all have the same priority
		expectedPri := addedValsPriSlice[0].ProposerPriority
		for _, val := range addedValsPriSlice[1:] {
			assert.Equal(t, expectedPri, val.ProposerPriority)
		}
	}
}

func TestNewValidatorSetFromExistingValidators(t *testing.T) {
	size := 5
	vals := make([]*Validator, size)
	for i := 0; i < size; i++ {
		pv := NewMockPV(PvKeyComposite) // TODO 🏺 need to test by all key types
		vals[i] = pv.ExtractIntoValidator(int64(i + 1))
	}
	valSet := NewValidatorSet(vals)
	valSet.IncrementProposerPriority(5)

	newValSet := NewValidatorSet(valSet.Validators)
	assert.NotEqual(t, valSet, newValSet)

	existingValSet, err := ValidatorSetFromExistingValidators(valSet.Validators)
	assert.NoError(t, err)
	assert.Equal(t, valSet, existingValSet)
	assert.Equal(t, valSet.CopyIncrementProposerPriority(3), existingValSet.CopyIncrementProposerPriority(3))
}

func TestValSetUpdateOverflowRelated(t *testing.T) {
	testCases := []testVSetCfg{
		{
			name:         "1 no false overflow error messages for updates",
			startVals:    []testVal{{"v2", MaxTotalStakingPower - 1}, {"v1", 1}},
			updatedVals:  []testVal{{"v1", MaxTotalStakingPower - 1}, {"v2", 1}},
			expectedVals: []testVal{{"v1", MaxTotalStakingPower - 1}, {"v2", 1}},
			expErr:       nil,
		},
		{
			// this test shows that it is important to apply the updates in the order of the change in power
			// i.e. apply first updates with decreases in power, v2 change in this case.
			name:         "2 no false overflow error messages for updates",
			startVals:    []testVal{{"v2", MaxTotalStakingPower - 1}, {"v1", 1}},
			updatedVals:  []testVal{{"v1", MaxTotalStakingPower/2 - 1}, {"v2", MaxTotalStakingPower / 2}},
			expectedVals: []testVal{{"v2", MaxTotalStakingPower / 2}, {"v1", MaxTotalStakingPower/2 - 1}},
			expErr:       nil,
		},
		{
			name:         "3 no false overflow error messages for deletes",
			startVals:    []testVal{{"v1", MaxTotalStakingPower - 2}, {"v2", 1}, {"v3", 1}},
			deletedVals:  []testVal{{"v1", 0}},
			addedVals:    []testVal{{"v4", MaxTotalStakingPower - 2}},
			expectedVals: []testVal{{"v4", MaxTotalStakingPower - 2}, {"v2", 1}, {"v3", 1}},
			expErr:       nil,
		},
		{
			name: "4 no false overflow error messages for adds, updates and deletes",
			startVals: []testVal{
				{"v1", MaxTotalStakingPower / 4}, {"v2", MaxTotalStakingPower / 4},
				{"v3", MaxTotalStakingPower / 4}, {"v4", MaxTotalStakingPower / 4}},
			deletedVals: []testVal{{"v2", 0}},
			updatedVals: []testVal{
				{"v1", MaxTotalStakingPower/2 - 2}, {"v3", MaxTotalStakingPower/2 - 3}, {"v4", 2}},
			addedVals: []testVal{{"v5", 3}},
			expectedVals: []testVal{
				{"v1", MaxTotalStakingPower/2 - 2}, {"v3", MaxTotalStakingPower/2 - 3}, {"v5", 3}, {"v4", 2}},
			expErr: nil,
		},
		{
			name: "5 check panic on overflow is prevented: update 8 validators with power int64(math.MaxInt64)/8",
			startVals: []testVal{
				{"v1", 1}, {"v2", 1}, {"v3", 1}, {"v4", 1}, {"v5", 1},
				{"v6", 1}, {"v7", 1}, {"v8", 1}, {"v9", 1}},
			updatedVals: []testVal{
				{"v1", MaxTotalStakingPower}, {"v2", MaxTotalStakingPower}, {"v3", MaxTotalStakingPower},
				{"v4", MaxTotalStakingPower}, {"v5", MaxTotalStakingPower}, {"v6", MaxTotalStakingPower},
				{"v7", MaxTotalStakingPower}, {"v8", MaxTotalStakingPower}, {"v9", 8}},
			expectedVals: []testVal{
				{"v1", 1}, {"v2", 1}, {"v3", 1}, {"v4", 1}, {"v5", 1},
				{"v6", 1}, {"v7", 1}, {"v8", 1}, {"v9", 1}},
			expErr: ErrTotalVotingPowerOverflow,
		},
	}

	for _, tt := range testCases {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			valSet := createNewValidatorSet(tt.startVals)
			verifyValidatorSet(t, valSet)

			// execute update and verify returned error is as expected
			applyChangesToValSet(t, tt.expErr, valSet, tt.addedVals, tt.updatedVals, tt.deletedVals)

			// verify updated validator set is as expected
			assert.Equal(t, tt.expectedVals, toTestValList(valSet.Validators))
			verifyValidatorSet(t, valSet)
		})
	}
}

func TestVerifyCommitTrusting(t *testing.T) {
	var (
		blockID                                      = makeBlockIDRandom()
		voteSet, _, originalVoterSet, privValidators = randVoteSet(1, 1, tmproto.PrecommitType, 6, 1)
		commit, err                                  = MakeCommit(blockID, 1, 1, voteSet, privValidators, time.Now())
		_, newVoterSet, _                            = RandVoterSet(2, 1)
	)
	require.NoError(t, err)

	testCases := []struct {
		// valSet *ValidatorSet
		voterSet *VoterSet
		err      bool
	}{
		// good
		0: {
			//valSet: originalValset,
			voterSet: originalVoterSet,
			err:      false,
		},
		// bad - no overlap between validator sets
		1: {
			voterSet: newVoterSet,
			err:      true,
		},
		// good - first two are different but the rest of the same -> >1/3
		2: {
			//voterSet: WrapValidatorsToVoterSet(append(newValSet.Validators, originalValset.Validators...)),
			voterSet: WrapValidatorsToVoterSet(append(newVoterSet.Voters, originalVoterSet.Voters...)),
			err:      false,
		},
	}

	for _, tc := range testCases {
		err = tc.voterSet.VerifyCommitLightTrusting("test_chain_id", commit,
			tmmath.Fraction{Numerator: 1, Denominator: 3})
		if tc.err {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
}

func TestVerifyCommitTrustingErrorsOnOverflow(t *testing.T) {
	var (
		blockID                    = makeBlockIDRandom()
		voteSet, _, voterSet, vals = randVoteSet(1, 1, tmproto.PrecommitType, 1, MaxTotalStakingPower)
		commit, err                = MakeCommit(blockID, 1, 1, voteSet, vals, time.Now())
	)
	require.NoError(t, err)

	err = voterSet.VerifyCommitLightTrusting("test_chain_id", commit,
		tmmath.Fraction{Numerator: 25, Denominator: 55})
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "int64 overflow")
	}
}

func TestSafeMul(t *testing.T) {
	testCases := []struct {
		a        int64
		b        int64
		c        int64
		overflow bool
	}{
		0: {0, 0, 0, false},
		1: {1, 0, 0, false},
		2: {2, 3, 6, false},
		3: {2, -3, -6, false},
		4: {-2, -3, 6, false},
		5: {-2, 3, -6, false},
		6: {math.MaxInt64, 1, math.MaxInt64, false},
		7: {math.MaxInt64 / 2, 2, math.MaxInt64 - 1, false},
		8: {math.MaxInt64 / 2, 3, 0, true},
		9: {math.MaxInt64, 2, 0, true},
	}

	for i, tc := range testCases {
		c, overflow := safeMul(tc.a, tc.b)
		assert.Equal(t, tc.c, c, "#%d", i)
		assert.Equal(t, tc.overflow, overflow, "#%d", i)
	}
}

func TestValidatorSetProtoBuf(t *testing.T) {
	valset, _ := RandValidatorSet(10, 100)
	valset2, _ := RandValidatorSet(10, 100)
	valset2.Validators[0] = &Validator{}

	testCases := []struct {
		msg      string
		v1       *ValidatorSet
		expPass1 bool
		expPass2 bool
	}{
		{"success", valset, true, true},
		{"fail valSet2, pubkey empty", valset2, false, false},
		{"fail empty valSet", &ValidatorSet{}, true, false},
		{"false nil", nil, true, false},
	}
	for _, tc := range testCases {
		protoValSet, err := tc.v1.ToProto()
		if tc.expPass1 {
			require.NoError(t, err, tc.msg)
		} else {
			require.Error(t, err, tc.msg)
		}

		valSet, err := ValidatorSetFromProto(protoValSet)
		if tc.expPass2 {
			require.NoError(t, err, tc.msg)
			require.EqualValues(t, tc.v1, valSet, tc.msg)
		} else {
			require.Error(t, err, tc.msg)
		}
	}
}

//---------------------
// Sort validators by priority and address
type validatorsByPriority []*Validator

func (valz validatorsByPriority) Len() int {
	return len(valz)
}

func (valz validatorsByPriority) Less(i, j int) bool {
	if valz[i].ProposerPriority < valz[j].ProposerPriority {
		return true
	}
	if valz[i].ProposerPriority > valz[j].ProposerPriority {
		return false
	}
	return bytes.Compare(valz[i].Address, valz[j].Address) < 0
}

func (valz validatorsByPriority) Swap(i, j int) {
	valz[i], valz[j] = valz[j], valz[i]
}

//-------------------------------------

type testValsByVotingPower []testVal

func (tvals testValsByVotingPower) Len() int {
	return len(tvals)
}

func (tvals testValsByVotingPower) Less(i, j int) bool {
	if tvals[i].power == tvals[j].power {
		return bytes.Compare([]byte(tvals[i].name), []byte(tvals[j].name)) == -1
	}
	return tvals[i].power > tvals[j].power
}

func (tvals testValsByVotingPower) Swap(i, j int) {
	tvals[i], tvals[j] = tvals[j], tvals[i]
}

//-------------------------------------
// Benchmark tests
//
func BenchmarkUpdates(b *testing.B) {
	const (
		n = 100
		m = 2000
	)
	// Init with n validators
	vs := make([]*Validator, n)
	for j := 0; j < n; j++ {
		vs[j] = newValidator([]byte(fmt.Sprintf("v%d", j)), 100)
	}
	valSet := NewValidatorSet(vs)
	l := len(valSet.Validators)

	// Make m new validators
	newValList := make([]*Validator, m)
	for j := 0; j < m; j++ {
		newValList[j] = newValidator([]byte(fmt.Sprintf("v%d", j+l)), 1000)
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Add m validators to valSetCopy
		valSetCopy := valSet.Copy()
		assert.NoError(b, valSetCopy.UpdateWithChangeSet(newValList))
	}
}
