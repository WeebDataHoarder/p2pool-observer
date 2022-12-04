package sidechain

import (
	"encoding/json"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"strconv"
)

type NetworkType int

const (
	NetworkInvalid NetworkType = iota
	NetworkMainnet
	NetworkTestnet
	NetworkStagenet
)

const (
	PPLNSWindow     = 2160
	BlockTime       = 10
	UnclePenalty    = 20
	UncleBlockDepth = 3
)

type ConsensusProvider interface {
	Consensus() *Consensus
}

func (n NetworkType) String() string {
	switch n {
	case NetworkInvalid:
		return "invalid"
	case NetworkMainnet:
		return "mainnet"
	case NetworkTestnet:
		return "testnet"
	case NetworkStagenet:
		return "stagenet"
	}
	return ""
}

func (n NetworkType) MarshalJSON() ([]byte, error) {
	return json.Marshal(n.String())
}

func (n *NetworkType) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}

	switch s {
	case "invalid":
		*n = NetworkInvalid
	case "", "mainnet": //special case for config.json
		*n = NetworkMainnet
	case "testnet":
		*n = NetworkTestnet
	case "stagenet":
		*n = NetworkStagenet

	default:
		return fmt.Errorf("unknown network type %s", s)
	}

	return nil
}

type Consensus struct {
	NetworkType       NetworkType `json:"network_type"`
	PoolName          string      `json:"name"`
	PoolPassword      string      `json:"password"`
	TargetBlockTime   uint64      `json:"block_time"`
	MinimumDifficulty uint64      `json:"min_diff"`
	ChainWindowSize   uint64      `json:"pplns_window"`
	UnclePenalty      uint64      `json:"uncle_penalty"`

	id types.Hash
}

const SmallestMinimumDifficulty = 100000
const LargestMinimumDifficulty = 1000000000

func NewConsensus(networkType NetworkType, poolName, poolPassword string, targetBlockTime, minimumDifficulty, chainWindowSize, unclePenalty uint64) *Consensus {
	c := &Consensus{
		NetworkType:       networkType,
		PoolName:          poolName,
		PoolPassword:      poolPassword,
		TargetBlockTime:   targetBlockTime,
		MinimumDifficulty: minimumDifficulty,
		ChainWindowSize:   chainWindowSize,
		UnclePenalty:      unclePenalty,
	}

	if len(c.PoolName) > 128 {
		return nil
	}

	if len(c.PoolPassword) > 128 {
		return nil
	}

	if c.TargetBlockTime < 1 || c.TargetBlockTime > monero.BlockTime {
		return nil
	}

	if c.MinimumDifficulty < SmallestMinimumDifficulty || c.MinimumDifficulty > LargestMinimumDifficulty {
		return nil
	}

	if c.ChainWindowSize < 60 || c.ChainWindowSize > 2160 {
		return nil
	}

	if c.UnclePenalty < 1 || c.UnclePenalty > 99 {
		return nil
	}

	var emptyHash types.Hash
	c.id = c.CalculateId()
	if c.id == emptyHash {
		return nil
	}
	return c
}

func (i *Consensus) CalculateSideTemplateId(main *mainblock.Block, side *SideData) types.Hash {

	mainData, _ := main.SideChainHashingBlob()
	sideData, _ := side.MarshalBinary()

	return i.CalculateSideChainIdFromBlobs(mainData, sideData)
}

func (i *Consensus) CalculateSideChainIdFromBlobs(mainBlob, sideBlob []byte) types.Hash {
	return crypto.PooledKeccak256(mainBlob, sideBlob, i.id[:])
}

func (i *Consensus) Id() types.Hash {
	var h types.Hash
	if i.id == h {
		//this data race is fine
		i.id = i.CalculateId()
		return i.id
	}
	return i.id
}

func (i *Consensus) IsDefault() bool {
	return i.id == ConsensusDefault.id
}

func (i *Consensus) IsMini() bool {
	return i.id == ConsensusMini.id
}

func (i *Consensus) DefaultPort() uint16 {
	if i.IsMini() {
		return 37888
	}
	return 37889
}

func (i *Consensus) InitialHintURL() string {
	//TODO: do not require this later on
	if i.IsMini() {
		return "https://mini.p2pool.observer/api/shares?limit=2160&onlyBlocks"
	} else if i.IsDefault() {
		return "https://p2pool.observer/api/shares?limit=2160&onlyBlocks"
	}
	return ""
}

func (i *Consensus) CalculateId() types.Hash {
	var buf []byte
	buf = append(buf, i.NetworkType.String()...)
	buf = append(buf, 0)
	buf = append(buf, i.PoolName...)
	buf = append(buf, 0)
	buf = append(buf, i.PoolPassword...)
	buf = append(buf, 0)
	buf = append(buf, strconv.FormatUint(i.TargetBlockTime, 10)...)
	buf = append(buf, 0)
	buf = append(buf, strconv.FormatUint(i.MinimumDifficulty, 10)...)
	buf = append(buf, 0)
	buf = append(buf, strconv.FormatUint(i.ChainWindowSize, 10)...)
	buf = append(buf, 0)
	buf = append(buf, strconv.FormatUint(i.UnclePenalty, 10)...)
	buf = append(buf, 0)

	return randomx.ConsensusHash(buf)
}

var ConsensusDefault = &Consensus{NetworkType: NetworkMainnet, PoolName: "mainnet test 2", TargetBlockTime: 10, MinimumDifficulty: 100000, ChainWindowSize: 2160, UnclePenalty: 20, id: types.Hash{34, 175, 126, 231, 181, 11, 104, 146, 227, 153, 218, 107, 44, 108, 68, 39, 178, 81, 4, 212, 169, 4, 142, 0, 177, 110, 157, 240, 68, 7, 249, 24}}
var ConsensusMini = &Consensus{NetworkType: NetworkMainnet, PoolName: "mini", TargetBlockTime: 10, MinimumDifficulty: 100000, ChainWindowSize: 2160, UnclePenalty: 20, id: types.Hash{57, 130, 201, 26, 149, 174, 199, 250, 66, 80, 189, 18, 108, 216, 194, 220, 136, 23, 63, 24, 64, 113, 221, 44, 219, 86, 39, 163, 53, 24, 126, 196}}
