package database

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	p2poolBlock "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/holiman/uint256"
	"golang.org/x/exp/slices"
	"lukechampine.com/uint128"
	"math/bits"
	"sync"
)

var NilHash types.Hash
var UndefinedHash types.Hash
var UndefinedDifficulty types.Difficulty

func init() {
	copy(NilHash[:], bytes.Repeat([]byte{0}, types.HashSize))
	copy(UndefinedHash[:], bytes.Repeat([]byte{0xff}, types.HashSize))
	UndefinedDifficulty.Uint128 = uint128.FromBytes(bytes.Repeat([]byte{0xff}, types.DifficultySize))
}

type BlockInterface interface {
	GetBlock() *Block
}

type BlockCoinbase struct {
	Id         types.Hash `json:"id"`
	Reward     uint64     `json:"reward"`
	PrivateKey types.Hash `json:"private_key"`
	//Payouts extra JSON field, do not use
	Payouts []*JSONCoinbaseOutput `json:"payouts,omitempty"`
}

type BlockMainData struct {
	Id     types.Hash `json:"id"`
	Height uint64     `json:"height"`
	Found  bool       `json:"found"`

	//Orphan extra JSON field, do not use
	Orphan bool `json:"orphan,omitempty"`
}

type JSONBlockParent struct {
	Id     types.Hash `json:"id"`
	Height uint64     `json:"height"`
}

type JSONUncleBlockSimple struct {
	Id     types.Hash `json:"id"`
	Height uint64     `json:"height"`
	Weight uint64     `json:"weight"`
}

type JSONCoinbaseOutput struct {
	Amount  uint64 `json:"amount"`
	Index   uint64 `json:"index"`
	Address string `json:"address"`
}

type JSONUncleBlockExtra struct {
	Id         types.Hash       `json:"id"`
	Height     uint64           `json:"height"`
	Difficulty types.Difficulty `json:"difficulty"`
	Timestamp  uint64           `json:"timestamp"`
	Miner      string           `json:"miner"`
	PowHash    types.Hash       `json:"pow"`
	Weight     uint64           `json:"weight"`
}

type Block struct {
	Id         types.Hash       `json:"id"`
	Height     uint64           `json:"height"`
	PreviousId types.Hash       `json:"previous_id"`
	Coinbase   BlockCoinbase    `json:"coinbase"`
	Difficulty types.Difficulty `json:"difficulty"`
	Timestamp  uint64           `json:"timestamp"`
	MinerId    uint64           `json:"-"`
	//Address extra JSON field, do not use
	Address string     `json:"miner,omitempty"`
	PowHash types.Hash `json:"pow"`

	Main BlockMainData `json:"main"`

	Template struct {
		Id         types.Hash       `json:"id"`
		Difficulty types.Difficulty `json:"difficulty"`
	} `json:"template"`

	//Lock extra JSON field, do not use
	Lock sync.Mutex `json:"-"`

	//Weight extra JSON field, do not use
	Weight uint64 `json:"weight"`
	//Parent extra JSON field, do not use
	Parent *JSONBlockParent `json:"parent,omitempty"`
	//Uncles extra JSON field, do not use
	Uncles []any `json:"uncles,omitempty"`

	//Orphan extra JSON field, do not use
	Orphan bool `json:"orphan,omitempty"`

	//Invalid extra JSON field, do not use
	Invalid *bool `json:"invalid,omitempty"`
}

func NewBlockFromBinaryBlock(db *Database, b *p2poolBlock.Block, knownUncles []*p2poolBlock.Block, errOnUncles bool) (block *Block, uncles []*UncleBlock, err error) {
	miner := db.GetOrCreateMinerByAddress(b.GetAddress().ToBase58())
	if miner == nil {
		return nil, nil, errors.New("could not get or create miner")
	}

	block = &Block{
		Id:         b.Main.CoinbaseExtra.SideId,
		Height:     b.Side.Height,
		PreviousId: b.Side.Parent,
		Coinbase: BlockCoinbase{
			Id: b.Main.Coinbase.Id(),
			Reward: func() (v uint64) {
				for _, o := range b.Main.Coinbase.Outputs {
					v += o.Reward
				}
				return
			}(),
			PrivateKey: b.Side.CoinbasePrivateKey,
		},
		Difficulty: b.Side.Difficulty,
		Timestamp:  b.Main.Timestamp,
		MinerId:    miner.Id(),
		PowHash:    b.Extra.PowHash,
		Main: BlockMainData{
			Id:     b.Extra.MainId,
			Height: b.Main.Coinbase.GenHeight,
			Found:  b.IsProofHigherThanDifficulty(),
		},
		Template: struct {
			Id         types.Hash       `json:"id"`
			Difficulty types.Difficulty `json:"difficulty"`
		}{
			Id:         b.Main.Parent,
			Difficulty: b.Extra.MainDifficulty,
		},
	}

	for _, u := range b.Side.Uncles {
		if i := slices.IndexFunc(knownUncles, func(uncle *p2poolBlock.Block) bool {
			return uncle.Main.CoinbaseExtra.SideId == u
		}); i != -1 {
			uncle := knownUncles[i]
			uncleMiner := db.GetOrCreateMinerByAddress(uncle.GetAddress().ToBase58())
			if uncleMiner == nil {
				return nil, nil, errors.New("could not get or create miner")
			}
			uncles = append(uncles, &UncleBlock{
				Block: Block{
					Id:         uncle.Main.CoinbaseExtra.SideId,
					Height:     uncle.Side.Height,
					PreviousId: uncle.Side.Parent,
					Coinbase: BlockCoinbase{
						Id: uncle.Main.Coinbase.Id(),
						Reward: func() (v uint64) {
							for _, o := range uncle.Main.Coinbase.Outputs {
								v += o.Reward
							}
							return
						}(),
						PrivateKey: uncle.Side.CoinbasePrivateKey,
					},
					Difficulty: uncle.Side.Difficulty,
					Timestamp:  uncle.Main.Timestamp,
					MinerId:    uncleMiner.Id(),
					PowHash:    uncle.Extra.PowHash,
					Main: BlockMainData{
						Id:     uncle.Extra.MainId,
						Height: uncle.Main.Coinbase.GenHeight,
						Found:  uncle.IsProofHigherThanDifficulty(),
					},
					Template: struct {
						Id         types.Hash       `json:"id"`
						Difficulty types.Difficulty `json:"difficulty"`
					}{
						Id:         uncle.Main.Parent,
						Difficulty: uncle.Extra.MainDifficulty,
					},
				},
				ParentId:     block.Id,
				ParentHeight: block.Height,
			})
		} else if errOnUncles {
			return nil, nil, fmt.Errorf("could not find uncle %s", hex.EncodeToString(u[:]))
		}
	}

	return block, uncles, nil
}

type JsonBlock2 struct {
	MinerMainId    types.Hash       `json:"miner_main_id"`
	CoinbaseReward uint64           `json:"coinbase_reward,string"`
	CoinbaseId     types.Hash       `json:"coinbase_id"`
	Version        uint64           `json:"version,string"`
	Diff           types.Difficulty `json:"diff"`
	Wallet         *address.Address `json:"wallet"`
	MinerMainDiff  types.Difficulty `json:"miner_main_diff"`
	Id             types.Hash       `json:"id"`
	Height         uint64           `json:"height,string"`
	PowHash        types.Hash       `json:"pow_hash"`
	MainId         types.Hash       `json:"main_id"`
	MainHeight     uint64           `json:"main_height,string"`
	Ts             uint64           `json:"ts,string"`
	PrevId         types.Hash       `json:"prev_id"`
	CoinbasePriv   types.Hash       `json:"coinbase_priv"`
	Lts            uint64           `json:"lts,string"`
	MainFound      string           `json:"main_found,omitempty"`

	Uncles []struct {
		MinerMainId    types.Hash       `json:"miner_main_id"`
		CoinbaseReward uint64           `json:"coinbase_reward,string"`
		CoinbaseId     types.Hash       `json:"coinbase_id"`
		Version        uint64           `json:"version,string"`
		Diff           types.Difficulty `json:"diff"`
		Wallet         *address.Address `json:"wallet"`
		MinerMainDiff  types.Difficulty `json:"miner_main_diff"`
		Id             types.Hash       `json:"id"`
		Height         uint64           `json:"height,string"`
		PowHash        types.Hash       `json:"pow_hash"`
		MainId         types.Hash       `json:"main_id"`
		MainHeight     uint64           `json:"main_height,string"`
		Ts             uint64           `json:"ts,string"`
		PrevId         types.Hash       `json:"prev_id"`
		CoinbasePriv   types.Hash       `json:"coinbase_priv"`
		Lts            uint64           `json:"lts,string"`
		MainFound      string           `json:"main_found,omitempty"`
	} `json:"uncles,omitempty"`
}

type JsonBlock1 struct {
	Wallet     *address.Address `json:"wallet"`
	Height     uint64           `json:"height,string"`
	MHeight    uint64           `json:"mheight,string"`
	PrevId     types.Hash       `json:"prev_id"`
	Ts         uint64           `json:"ts,string"`
	PowHash    types.Hash       `json:"pow_hash"`
	Id         types.Hash       `json:"id"`
	PrevHash   types.Hash       `json:"prev_hash"`
	Diff       uint64           `json:"diff,string"`
	TxCoinbase types.Hash       `json:"tx_coinbase"`
	Lts        uint64           `json:"lts,string"`
	MHash      types.Hash       `json:"mhash"`
	TxPriv     types.Hash       `json:"tx_priv"`
	TxPub      types.Hash       `json:"tx_pub"`
	BlockFound string           `json:"main_found,omitempty"`

	Uncles []struct {
		Diff     uint64           `json:"diff,string"`
		PrevId   types.Hash       `json:"prev_id"`
		Ts       uint64           `json:"ts,string"`
		MHeight  uint64           `json:"mheight,string"`
		PrevHash types.Hash       `json:"prev_hash"`
		Height   uint64           `json:"height,string"`
		Wallet   *address.Address `json:"wallet"`
		Id       types.Hash       `json:"id"`
	} `json:"uncles,omitempty"`
}

type versionBlock struct {
	Version uint64 `json:"version,string"`
}

func NewBlockFromJSONBlock(db *Database, data []byte) (block *Block, uncles []*UncleBlock, err error) {
	var version versionBlock

	if err = json.Unmarshal(data, &version); err != nil {
		return nil, nil, err
	} else {
		if version.Version == 2 {

			var b JsonBlock2
			if err = json.Unmarshal(data, &b); err != nil {
				return nil, nil, err
			}

			miner := db.GetOrCreateMinerByAddress(b.Wallet.ToBase58())
			if miner == nil {
				return nil, nil, errors.New("could not get or create miner")
			}

			block = &Block{
				Id:         b.Id,
				Height:     b.Height,
				PreviousId: b.PrevId,
				Coinbase: BlockCoinbase{
					Id:         b.CoinbaseId,
					Reward:     b.CoinbaseReward,
					PrivateKey: b.CoinbasePriv,
				},
				Difficulty: b.Diff,
				Timestamp:  b.Ts,
				MinerId:    miner.Id(),
				PowHash:    b.PowHash,
				Main: BlockMainData{
					Id:     b.MainId,
					Height: b.MainHeight,
					Found:  b.MainFound == "true",
				},
				Template: struct {
					Id         types.Hash       `json:"id"`
					Difficulty types.Difficulty `json:"difficulty"`
				}{
					Id:         b.MinerMainId,
					Difficulty: b.MinerMainDiff,
				},
			}

			if block.IsProofHigherThanDifficulty() {
				block.Main.Found = true
			}

			for _, u := range b.Uncles {
				uncleMiner := db.GetOrCreateMinerByAddress(u.Wallet.ToBase58())
				if uncleMiner == nil {
					return nil, nil, errors.New("could not get or create miner")
				}

				uncle := &UncleBlock{
					Block: Block{
						Id:         u.Id,
						Height:     u.Height,
						PreviousId: u.PrevId,
						Coinbase: BlockCoinbase{
							Id:         u.CoinbaseId,
							Reward:     u.CoinbaseReward,
							PrivateKey: u.CoinbasePriv,
						},
						Difficulty: u.Diff,
						Timestamp:  u.Ts,
						MinerId:    uncleMiner.Id(),
						PowHash:    u.PowHash,
						Main: BlockMainData{
							Id:     u.MainId,
							Height: u.MainHeight,
							Found:  u.MainFound == "true",
						},
						Template: struct {
							Id         types.Hash       `json:"id"`
							Difficulty types.Difficulty `json:"difficulty"`
						}{
							Id:         u.MinerMainId,
							Difficulty: u.MinerMainDiff,
						},
					},
					ParentId:     block.Id,
					ParentHeight: block.Height,
				}

				if uncle.Block.IsProofHigherThanDifficulty() {
					uncle.Block.Main.Found = true
				}

				uncles = append(uncles, uncle)
			}
			return block, uncles, nil
		} else if version.Version == 0 || version.Version == 1 {

			var b JsonBlock1
			if err = json.Unmarshal(data, &b); err != nil {
				return nil, nil, err
			}

			miner := db.GetOrCreateMinerByAddress(b.Wallet.ToBase58())
			if miner == nil {
				return nil, nil, errors.New("could not get or create miner")
			}

			block = &Block{
				Id:         b.Id,
				Height:     b.Height,
				PreviousId: b.PrevId,
				Coinbase: BlockCoinbase{
					Id:         b.TxCoinbase,
					Reward:     0,
					PrivateKey: b.TxPriv,
				},
				Difficulty: types.Difficulty{Uint128: uint128.From64(b.Diff)},
				Timestamp:  b.Ts,
				MinerId:    miner.Id(),
				PowHash:    b.PowHash,
				Main: BlockMainData{
					Id:     b.MHash,
					Height: b.MHeight,
					Found:  b.BlockFound == "true",
				},
				Template: struct {
					Id         types.Hash       `json:"id"`
					Difficulty types.Difficulty `json:"difficulty"`
				}{
					Id:         UndefinedHash,
					Difficulty: UndefinedDifficulty,
				},
			}

			for _, u := range b.Uncles {
				uncleMiner := db.GetOrCreateMinerByAddress(u.Wallet.ToBase58())
				if uncleMiner == nil {
					return nil, nil, errors.New("could not get or create miner")
				}

				uncle := &UncleBlock{
					Block: Block{
						Id:         u.Id,
						Height:     u.Height,
						PreviousId: u.PrevId,
						Coinbase: BlockCoinbase{
							Id:         NilHash,
							Reward:     0,
							PrivateKey: NilHash,
						},
						Difficulty: types.Difficulty{Uint128: uint128.From64(b.Diff)},
						Timestamp:  u.Ts,
						MinerId:    uncleMiner.Id(),
						PowHash:    NilHash,
						Main: BlockMainData{
							Id:     NilHash,
							Height: 0,
							Found:  false,
						},
						Template: struct {
							Id         types.Hash       `json:"id"`
							Difficulty types.Difficulty `json:"difficulty"`
						}{
							Id:         UndefinedHash,
							Difficulty: UndefinedDifficulty,
						},
					},
					ParentId:     block.Id,
					ParentHeight: block.Height,
				}

				uncles = append(uncles, uncle)
			}
			return block, uncles, nil
		} else {
			return nil, nil, fmt.Errorf("unknown version %d", version.Version)
		}
	}
}

func (b *Block) GetBlock() *Block {
	return b
}

func (b *Block) IsProofHigherThanDifficulty() bool {
	return b.GetProofDifficulty().Cmp(b.Template.Difficulty.Uint128) >= 0
}

func (b *Block) GetProofDifficulty() types.Difficulty {
	base := uint256.NewInt(0).SetBytes32(bytes.Repeat([]byte{0xff}, 32))
	pow := uint256.NewInt(0).SetBytes32(b.PowHash[:])
	pow = &uint256.Int{bits.ReverseBytes64(pow[3]), bits.ReverseBytes64(pow[2]), bits.ReverseBytes64(pow[1]), bits.ReverseBytes64(pow[0])}

	if pow.Eq(uint256.NewInt(0)) {
		return types.Difficulty{}
	}

	powResult := uint256.NewInt(0).Div(base, pow).Bytes32()
	return types.Difficulty{Uint128: uint128.FromBytes(powResult[16:]).ReverseBytes()}
}
