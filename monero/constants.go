package monero

const (
	BlockTime                = 60 * 2
	HardForkViewTagsVersion  = 15
	HardForkSupportedVersion = 16

	TransactionUnlockTime = 10
	MinerRewardUnlockTime = 60

	TailEmissionReward = 600000000000

	RequiredMajor         = 3
	RequiredMinor         = 10
	RequiredMoneroVersion = (RequiredMajor << 16) | RequiredMinor
	RequiredMoneroString  = "v0.18.0.0"
)
