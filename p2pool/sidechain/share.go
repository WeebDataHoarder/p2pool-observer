package sidechain

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	mainblock "git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/holiman/uint256"
	"io"
	"lukechampine.com/uint128"
	"math/bits"
)

type Share struct {
	Main mainblock.Block

	Side SideData

	CoinbaseExtra struct {
		PublicKey  types.Hash
		ExtraNonce []byte
		SideId     types.Hash
	}

	Extra struct {
		MainId         types.Hash
		PowHash        types.Hash
		MainDifficulty types.Difficulty
		Peer           []byte
	}
}

func NewShareFromBytes(buf []byte) (*Share, error) {
	b := &Share{}
	return b, b.UnmarshalBinary(buf)
}

func (b *Share) TemplateId(consensus *Consensus) types.Hash {
	return consensus.CalculateSideChainId(&b.Main, &b.Side)
}

func (b *Share) UnmarshalBinary(data []byte) error {

	if len(data) < 32 {
		return errors.New("invalid block data")
	}

	reader := bytes.NewReader(data)

	var (
		err     error
		version uint64

		mainDataSize uint64
		mainData     []byte

		sideDataSize uint64
		sideData     []byte
	)

	if err = binary.Read(reader, binary.BigEndian, &version); err != nil {
		return err
	}

	switch version {
	case 1:

		if _, err = io.ReadFull(reader, b.Extra.MainId[:]); err != nil {
			return err
		}

		if _, err = io.ReadFull(reader, b.Extra.PowHash[:]); err != nil {
			return err
		}
		if err = binary.Read(reader, binary.BigEndian, &b.Extra.MainDifficulty.Hi); err != nil {
			return err
		}
		if err = binary.Read(reader, binary.BigEndian, &b.Extra.MainDifficulty.Lo); err != nil {
			return err
		}

		b.Extra.MainDifficulty.ReverseBytes()

		if err = binary.Read(reader, binary.BigEndian, &mainDataSize); err != nil {
			return err
		}
		mainData = make([]byte, mainDataSize)
		if _, err = io.ReadFull(reader, mainData); err != nil {
			return err
		}

		if err = binary.Read(reader, binary.BigEndian, &sideDataSize); err != nil {
			return err
		}
		sideData = make([]byte, sideDataSize)
		if _, err = io.ReadFull(reader, sideData); err != nil {
			return err
		}

		//Ignore error when unable to read peer
		_ = func() error {
			var peerSize uint64

			if err = binary.Read(reader, binary.BigEndian, &peerSize); err != nil {
				return err
			}
			b.Extra.Peer = make([]byte, peerSize)
			if _, err = io.ReadFull(reader, b.Extra.Peer); err != nil {
				return err
			}

			return nil
		}()

	case 0:
		if err = binary.Read(reader, binary.BigEndian, &mainDataSize); err != nil {
			return err
		}
		mainData = make([]byte, mainDataSize)
		if _, err = io.ReadFull(reader, mainData); err != nil {
			return err
		}
		if sideData, err = io.ReadAll(reader); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown block version %d", version)
	}

	if err = b.Main.UnmarshalBinary(mainData); err != nil {
		return err
	}

	if err = b.Side.UnmarshalBinary(sideData); err != nil {
		return err
	}

	if err = b.fillFromTxExtra(); err != nil {
		return err
	}

	return nil
}

func (b *Share) fillFromTxExtra() error {

	for _, tag := range b.Main.Coinbase.Extra {
		switch tag.Tag {
		case transaction.TxExtraTagNonce:
			b.CoinbaseExtra.ExtraNonce = tag.Data
		case transaction.TxExtraTagMergeMining:
			if len(tag.Data) != types.HashSize {
				return fmt.Errorf("hash size %d is not %d", len(tag.Data), types.HashSize)
			}
			b.CoinbaseExtra.SideId = types.HashFromBytes(tag.Data)
		}
	}
	return nil
}

func (b *Share) IsProofHigherThanDifficulty() bool {
	return b.GetProofDifficulty().Cmp(b.Extra.MainDifficulty.Uint128) >= 0
}

func (b *Share) GetProofDifficulty() types.Difficulty {
	base := uint256.NewInt(0).SetBytes32(bytes.Repeat([]byte{0xff}, 32))
	pow := uint256.NewInt(0).SetBytes32(b.Extra.PowHash[:])
	pow = &uint256.Int{bits.ReverseBytes64(pow[3]), bits.ReverseBytes64(pow[2]), bits.ReverseBytes64(pow[1]), bits.ReverseBytes64(pow[0])}

	if pow.Eq(uint256.NewInt(0)) {
		return types.Difficulty{}
	}

	powResult := uint256.NewInt(0).Div(base, pow).Bytes32()
	return types.Difficulty{Uint128: uint128.FromBytes(powResult[16:]).ReverseBytes()}
}

func (b *Share) GetAddress() *address.Address {
	return address.FromRawAddress(moneroutil.MainNetwork, b.Side.PublicSpendKey, b.Side.PublicViewKey)
}
