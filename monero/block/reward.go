package block

import (
	"lukechampine.com/uint128"
	"math"
)

func GetBlockReward(medianWeight, currentBlockWeight, alreadyGeneratedCoins uint64, version uint8) (reward uint64) {
	const DIFFICULTY_TARGET_V1 = 60  // seconds - before first fork
	const DIFFICULTY_TARGET_V2 = 120 // seconds
	const EMISSION_SPEED_FACTOR_PER_MINUTE = 20
	const FINAL_SUBSIDY_PER_MINUTE = 300000000000 // 3 * pow(10, 11)
	const MONEY_SUPPLY = math.MaxUint64
	const CRYPTONOTE_BLOCK_GRANTED_FULL_REWARD_ZONE_V1 = 20000  //size of block (bytes) after which reward for block calculated using block size - before first fork
	const CRYPTONOTE_BLOCK_GRANTED_FULL_REWARD_ZONE_V2 = 60000  //size of block (bytes) after which reward for block calculated using block size
	const CRYPTONOTE_BLOCK_GRANTED_FULL_REWARD_ZONE_V5 = 300000 //size of block (bytes) after which reward for block calculated using block size - second change, from v5

	target := uint64(DIFFICULTY_TARGET_V2)
	if version < 2 {
		target = DIFFICULTY_TARGET_V1
	}

	targetMinutes := target / 60

	emissionSpeedFactor := EMISSION_SPEED_FACTOR_PER_MINUTE - (targetMinutes - 1)

	baseReward := (MONEY_SUPPLY - alreadyGeneratedCoins) >> emissionSpeedFactor
	if baseReward < (FINAL_SUBSIDY_PER_MINUTE * targetMinutes) {
		baseReward = FINAL_SUBSIDY_PER_MINUTE * targetMinutes
	}

	fullRewardZone := func(version uint8) uint64 {
		if version < 2 {
			return CRYPTONOTE_BLOCK_GRANTED_FULL_REWARD_ZONE_V1
		}
		if version < 5 {
			return CRYPTONOTE_BLOCK_GRANTED_FULL_REWARD_ZONE_V2
		}
		return CRYPTONOTE_BLOCK_GRANTED_FULL_REWARD_ZONE_V5
	}(version)

	//make it soft
	if medianWeight < fullRewardZone {
		medianWeight = fullRewardZone
	}

	if currentBlockWeight <= medianWeight {
		return baseReward
	}

	if currentBlockWeight > 2*medianWeight {
		//Block cumulative weight is too big
		return 0
	}

	product := uint128.From64(baseReward).Mul64(2*medianWeight - currentBlockWeight)
	return product.Div64(medianWeight).Div64(medianWeight).Lo
}
