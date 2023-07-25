package sidechain

type HardFork struct {
	Version   uint8  `json:"version"`
	Height    uint64 `json:"height"`
	Threshold uint8  `json:"threshold"`
	Time      uint64 `json:"time"`
}

var mainNetHardForks = []HardFork{
	// version 1 from the start of the blockchain
	{1, 1, 0, 1341378000},

	// version 2 starts from block 1009827, which is on or around the 20th of March, 2016. Fork time finalised on 2015-09-20. No fork voting occurs for the v2 fork.
	{2, 1009827, 0, 1442763710},

	// version 3 starts from block 1141317, which is on or around the 24th of September, 2016. Fork time finalised on 2016-03-21.
	{3, 1141317, 0, 1458558528},

	// version 4 starts from block 1220516, which is on or around the 5th of January, 2017. Fork time finalised on 2016-09-18.
	{4, 1220516, 0, 1483574400},

	// version 5 starts from block 1288616, which is on or around the 15th of April, 2017. Fork time finalised on 2017-03-14.
	{5, 1288616, 0, 1489520158},

	// version 6 starts from block 1400000, which is on or around the 16th of September, 2017. Fork time finalised on 2017-08-18.
	{6, 1400000, 0, 1503046577},

	// version 7 starts from block 1546000, which is on or around the 6th of April, 2018. Fork time finalised on 2018-03-17.
	{7, 1546000, 0, 1521303150},

	// version 8 starts from block 1685555, which is on or around the 18th of October, 2018. Fork time finalised on 2018-09-02.
	{8, 1685555, 0, 1535889547},

	// version 9 starts from block 1686275, which is on or around the 19th of October, 2018. Fork time finalised on 2018-09-02.
	{9, 1686275, 0, 1535889548},

	// version 10 starts from block 1788000, which is on or around the 9th of March, 2019. Fork time finalised on 2019-02-10.
	{10, 1788000, 0, 1549792439},

	// version 11 starts from block 1788720, which is on or around the 10th of March, 2019. Fork time finalised on 2019-02-15.
	{11, 1788720, 0, 1550225678},

	// version 12 starts from block 1978433, which is on or around the 30th of November, 2019. Fork time finalised on 2019-10-18.
	{12, 1978433, 0, 1571419280},

	{13, 2210000, 0, 1598180817},
	{14, 2210720, 0, 1598180818},

	{15, 2688888, 0, 1656629117},
	{16, 2689608, 0, 1656629118},
}

var testNetHardForks = []HardFork{
	// version 1 from the start of the blockchain
	{1, 1, 0, 1341378000},

	// version 2 starts from block 624634, which is on or around the 23rd of November, 2015. Fork time finalised on 2015-11-20. No fork voting occurs for the v2 fork.
	{2, 624634, 0, 1445355000},

	// versions 3-5 were passed in rapid succession from September 18th, 2016
	{3, 800500, 0, 1472415034},
	{4, 801219, 0, 1472415035},
	{5, 802660, 0, 1472415036 + 86400*180}, // add 5 months on testnet to shut the update warning up since there's a large gap to v6

	{6, 971400, 0, 1501709789},
	{7, 1057027, 0, 1512211236},
	{8, 1057058, 0, 1533211200},
	{9, 1057778, 0, 1533297600},
	{10, 1154318, 0, 1550153694},
	{11, 1155038, 0, 1550225678},
	{12, 1308737, 0, 1569582000},
	{13, 1543939, 0, 1599069376},
	{14, 1544659, 0, 1599069377},
	{15, 1982800, 0, 1652727000},
	{16, 1983520, 0, 1652813400},
}

var stageNetHardForks = []HardFork{
	// version 1 from the start of the blockchain
	{1, 1, 0, 1341378000},

	// versions 2-7 in rapid succession from March 13th, 2018
	{2, 32000, 0, 1521000000},
	{3, 33000, 0, 1521120000},
	{4, 34000, 0, 1521240000},
	{5, 35000, 0, 1521360000},
	{6, 36000, 0, 1521480000},
	{7, 37000, 0, 1521600000},
	{8, 176456, 0, 1537821770},
	{9, 177176, 0, 1537821771},
	{10, 269000, 0, 1550153694},
	{11, 269720, 0, 1550225678},
	{12, 454721, 0, 1571419280},
	{13, 675405, 0, 1598180817},
	{14, 676125, 0, 1598180818},
	{15, 1151000, 0, 1656629117},
	{16, 1151720, 0, 1656629118},
}

var p2poolMainNetHardForks = []HardFork{
	{1, 0, 0, 0},
	// p2pool hardforks at 2023-03-18 21:00 UTC
	{2, 0, 0, 1679173200},
}

var p2poolTestNetHardForks = []HardFork{
	{1, 0, 0, 0},
	// p2pool hardforks at 2023-01-23 21:00 UTC
	{2, 0, 0, 1674507600},
	//alternate hardfork at 2023-03-07 20:00 UTC 1678219200
	//{2, 0, 0, 1678219200},
}

var p2poolStageNetHardForks = []HardFork{
	//always latest version
	{p2poolMainNetHardForks[len(p2poolMainNetHardForks)-1].Version, 0, 0, 0},
}

func NetworkHardFork(consensus *Consensus) []HardFork {
	switch consensus.NetworkType {
	case NetworkMainnet:
		return mainNetHardForks
	case NetworkTestnet:
		return testNetHardForks
	case NetworkStagenet:
		return stageNetHardForks
	default:
		panic("invalid network type for determining share version")
	}
}

func NetworkMajorVersion(consensus *Consensus, height uint64) uint8 {
	hardForks := NetworkHardFork(consensus)

	if len(hardForks) == 0 {
		return 0
	}

	result := hardForks[0].Version

	for _, f := range hardForks[1:] {
		if height < f.Height {
			break
		}
		result = f.Version
	}
	return result
}

func P2PoolShareVersion(consensus *Consensus, timestamp uint64) ShareVersion {
	hardForks := consensus.HardForks

	if len(hardForks) == 0 {
		return ShareVersion_None
	}

	result := hardForks[0].Version

	for _, f := range hardForks[1:] {
		if timestamp < f.Time {
			break
		}
		result = f.Version
	}
	return ShareVersion(result)
}
