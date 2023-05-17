package sidechain

import (
	"encoding/json"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"log"
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

func (n NetworkType) AddressNetwork() (uint8, error) {
	switch n {
	case NetworkInvalid:
		return 0, errors.New("invalid network")
	case NetworkMainnet:
		return moneroutil.MainNetwork, nil
	case NetworkTestnet:
		return moneroutil.TestNetwork, nil
	case NetworkStagenet:
		return moneroutil.StageNetwork, nil
	}
	return 0, errors.New("unknown network")
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
	// HardFork optional hardfork information for p2pool
	HardForks []HardFork `json:"hard_forks,omitempty"`

	hasher randomx.Hasher

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

	if !c.verify() {
		return nil
	}
	return c
}

func NewConsensusFromJSON(data []byte) (*Consensus, error) {
	var c Consensus
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}

	if !c.verify() {
		return nil, errors.New("could not verify")
	}

	return &c, nil
}

func (c *Consensus) verify() bool {
	if len(c.PoolName) > 128 {
		return false
	}

	if len(c.PoolPassword) > 128 {
		return false
	}

	if c.TargetBlockTime < 1 || c.TargetBlockTime > monero.BlockTime {
		return false
	}

	if c.NetworkType == NetworkMainnet && c.MinimumDifficulty < SmallestMinimumDifficulty || c.MinimumDifficulty > LargestMinimumDifficulty {
		return false
	}

	if c.ChainWindowSize < 60 || c.ChainWindowSize > 2160 {
		return false
	}

	if c.UnclePenalty < 1 || c.UnclePenalty > 99 {
		return false
	}

	var emptyHash types.Hash
	c.id = c.CalculateId()
	if c.id == emptyHash {
		return false
	}

	if len(c.HardForks) == 0 {
		switch c.NetworkType {
		case NetworkMainnet:
			c.HardForks = p2poolMainNetHardForks
		case NetworkTestnet:
			c.HardForks = p2poolTestNetHardForks
		case NetworkStagenet:
			c.HardForks = p2poolStageNetHardForks
		default:
			log.Panicf("invalid network type for determining hardfork")
		}
	}

	return true
}

func (c *Consensus) CalculateSideTemplateId(share *PoolBlock) types.Hash {

	mainData, _ := share.Main.SideChainHashingBlob(true)
	sideData, _ := share.Side.MarshalBinary(share.ShareVersion())

	return c.CalculateSideChainIdFromBlobs(mainData, sideData)
}

func (c *Consensus) CalculateSideChainIdFromBlobs(mainBlob, sideBlob []byte) types.Hash {
	//TODO: handle extra nonce
	return crypto.PooledKeccak256(mainBlob, sideBlob, c.id[:])
}

func (c *Consensus) Id() types.Hash {
	var h types.Hash
	if c.id == h {
		//this data race is fine
		c.id = c.CalculateId()
		return c.id
	}
	return c.id
}

func (c *Consensus) IsDefault() bool {
	return c.id == ConsensusDefault.id
}

func (c *Consensus) IsMini() bool {
	return c.id == ConsensusMini.id
}

func (c *Consensus) DefaultPort() uint16 {
	if c.IsMini() {
		return 37888
	}
	return 37889
}

func (c *Consensus) SeedNode() string {
	if c.IsMini() {
		return "seeds-mini.p2pool.io"
	} else if c.IsDefault() {
		return "seeds.p2pool.io"
	}
	return ""
}

func (c *Consensus) InitHasher(n int, flags ...randomx.Flag) error {
	if c.hasher != nil {
		c.hasher.Close()
	}
	var err error
	c.hasher, err = randomx.NewRandomX(n, flags...)
	if err != nil {
		return err
	}
	return nil
}

func (c *Consensus) GetHasher() randomx.Hasher {
	if c.hasher == nil {
		log.Panic("hasher has not been initialized in consensus")
	}
	return c.hasher
}

func (c *Consensus) CalculateId() types.Hash {
	var buf []byte
	buf = append(buf, c.NetworkType.String()...)
	buf = append(buf, 0)
	buf = append(buf, c.PoolName...)
	buf = append(buf, 0)
	buf = append(buf, c.PoolPassword...)
	buf = append(buf, 0)
	buf = append(buf, strconv.FormatUint(c.TargetBlockTime, 10)...)
	buf = append(buf, 0)
	buf = append(buf, strconv.FormatUint(c.MinimumDifficulty, 10)...)
	buf = append(buf, 0)
	buf = append(buf, strconv.FormatUint(c.ChainWindowSize, 10)...)
	buf = append(buf, 0)
	buf = append(buf, strconv.FormatUint(c.UnclePenalty, 10)...)
	buf = append(buf, 0)

	return randomx.ConsensusHash(buf)
}

var ConsensusDefault = &Consensus{NetworkType: NetworkMainnet, PoolName: "mainnet test 2", TargetBlockTime: 10, MinimumDifficulty: 100000, ChainWindowSize: 2160, UnclePenalty: 20, HardForks: p2poolMainNetHardForks, id: types.Hash{34, 175, 126, 231, 181, 11, 104, 146, 227, 153, 218, 107, 44, 108, 68, 39, 178, 81, 4, 212, 169, 4, 142, 0, 177, 110, 157, 240, 68, 7, 249, 24}}
var ConsensusMini = &Consensus{NetworkType: NetworkMainnet, PoolName: "mini", TargetBlockTime: 10, MinimumDifficulty: 100000, ChainWindowSize: 2160, UnclePenalty: 20, HardForks: p2poolMainNetHardForks, id: types.Hash{57, 130, 201, 26, 149, 174, 199, 250, 66, 80, 189, 18, 108, 216, 194, 220, 136, 23, 63, 24, 64, 113, 221, 44, 219, 86, 39, 163, 53, 24, 126, 196}}
