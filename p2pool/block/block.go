package block

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/holiman/uint256"
	"io"
	"lukechampine.com/uint128"
	"math/bits"
)

type Block struct {
	Main struct {
		MajorVersion uint8
		MinorVersion uint8
		Timestamp    uint64
		Parent       types.Hash
		Nonce        types.Nonce

		Coinbase *CoinbaseTransaction

		CoinbaseExtra struct {
			PublicKey  types.Hash
			ExtraNonce []byte
			SideId     types.Hash
		}

		Transactions []types.Hash
	}

	Side struct {
		PublicSpendKey       types.Hash
		PublicViewKey        types.Hash
		CoinbasePrivateKey   types.Hash
		Parent               types.Hash
		Uncles               []types.Hash
		Height               uint64
		Difficulty           types.Difficulty
		CumulativeDifficulty types.Difficulty
	}

	Extra struct {
		MainId         types.Hash
		PowHash        types.Hash
		MainDifficulty types.Difficulty
		Peer           []byte
	}
}

func NewBlockFromBytes(buf []byte) (*Block, error) {
	b := &Block{}
	return b, b.UnmarshalBinary(buf)
}

func (b *Block) UnmarshalBinary(data []byte) error {

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

	if err = b.unmarshalMainData(mainData); err != nil {
		return err
	}

	if err = b.unmarshalTxExtra(b.Main.Coinbase.Extra); err != nil {
		return err
	}

	if err = b.unmarshalSideData(sideData); err != nil {
		return err
	}

	return nil
}

func (b *Block) unmarshalMainData(data []byte) error {
	reader := bytes.NewReader(data)

	var (
		err             error
		txCount         uint64
		transactionHash types.Hash
	)

	if err = binary.Read(reader, binary.BigEndian, &b.Main.MajorVersion); err != nil {
		return err
	}
	if err = binary.Read(reader, binary.BigEndian, &b.Main.MinorVersion); err != nil {
		return err
	}

	if b.Main.Timestamp, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if _, err = io.ReadFull(reader, b.Main.Parent[:]); err != nil {
		return err
	}

	if _, err = io.ReadFull(reader, b.Main.Nonce[:]); err != nil {
		return err
	}

	// Coinbase Tx Decoding
	{
		b.Main.Coinbase = &CoinbaseTransaction{}
		if err = b.Main.Coinbase.FromReader(reader); err != nil {
			return err
		}
	}

	if txCount, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	if txCount < 8192 {
		b.Main.Transactions = make([]types.Hash, 0, txCount)
	}

	for i := 0; i < int(txCount); i++ {
		if _, err = io.ReadFull(reader, transactionHash[:]); err != nil {
			return err
		}
		//TODO: check if copy is needed
		b.Main.Transactions = append(b.Main.Transactions, transactionHash)
	}

	return nil

}

func (b *Block) unmarshalTxExtra(data []byte) error {
	reader := bytes.NewReader(data)

	var (
		err       error
		extraTag  uint8
		nonceSize uint64
		mergeSize uint8
	)

	for {
		if err = binary.Read(reader, binary.BigEndian, &extraTag); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		switch extraTag {
		default:
			fallthrough
		case TxExtraTagPadding, TxExtraTagAdditionalPubKeys:
			return fmt.Errorf("unknown extra tag %d", extraTag)
		case TxExtraTagPubKey:
			if _, err = io.ReadFull(reader, b.Main.CoinbaseExtra.PublicKey[:]); err != nil {
				return err
			}
		case TxExtraNonce:
			if nonceSize, err = binary.ReadUvarint(reader); err != nil {
				return err
			}

			b.Main.CoinbaseExtra.ExtraNonce = make([]byte, nonceSize)
			if _, err = io.ReadFull(reader, b.Main.CoinbaseExtra.ExtraNonce); err != nil {
				return err
			}
		case TxExtraMergeMiningTag:
			if err = binary.Read(reader, binary.BigEndian, &mergeSize); err != nil {
				return err
			}
			if mergeSize != types.HashSize {
				return fmt.Errorf("hash size %d is not %d", mergeSize, types.HashSize)
			}
			if _, err = io.ReadFull(reader, b.Main.CoinbaseExtra.SideId[:]); err != nil {
				return err
			}
		}
	}
}

func (b *Block) IsProofHigherThanDifficulty() bool {
	return b.GetProofDifficulty().Cmp(b.Extra.MainDifficulty.Uint128) >= 0
}

func (b *Block) GetProofDifficulty() types.Difficulty {
	base := uint256.NewInt(0).SetBytes32(bytes.Repeat([]byte{0xff}, 32))
	pow := uint256.NewInt(0).SetBytes32(b.Extra.PowHash[:])
	pow = &uint256.Int{bits.ReverseBytes64(pow[3]), bits.ReverseBytes64(pow[2]), bits.ReverseBytes64(pow[1]), bits.ReverseBytes64(pow[0])}

	if pow.Eq(uint256.NewInt(0)) {
		return types.Difficulty{}
	}

	powResult := uint256.NewInt(0).Div(base, pow).Bytes32()
	return types.Difficulty{Uint128: uint128.FromBytes(powResult[16:]).ReverseBytes()}
}

func (b *Block) GetAddress() *address.Address {
	return address.FromRawAddress(moneroutil.MainNetwork, b.Side.PublicSpendKey, b.Side.PublicViewKey)
}

func (b *Block) unmarshalSideData(data []byte) error {
	reader := bytes.NewReader(data)

	var (
		err        error
		uncleCount uint64
		uncleHash  types.Hash
	)
	if _, err = io.ReadFull(reader, b.Side.PublicSpendKey[:]); err != nil {
		return err
	}
	if _, err = io.ReadFull(reader, b.Side.PublicViewKey[:]); err != nil {
		return err
	}
	if _, err = io.ReadFull(reader, b.Side.CoinbasePrivateKey[:]); err != nil {
		return err
	}
	if _, err = io.ReadFull(reader, b.Side.Parent[:]); err != nil {
		return err
	}
	if uncleCount, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	for i := 0; i < int(uncleCount); i++ {
		if _, err = io.ReadFull(reader, uncleHash[:]); err != nil {
			return err
		}
		//TODO: check if copy is needed
		b.Side.Uncles = append(b.Side.Uncles, uncleHash)
	}

	if b.Side.Height, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	{
		if b.Side.Difficulty.Lo, err = binary.ReadUvarint(reader); err != nil {
			return err
		}

		if b.Side.Difficulty.Hi, err = binary.ReadUvarint(reader); err != nil {
			return err
		}
	}

	{
		if b.Side.CumulativeDifficulty.Lo, err = binary.ReadUvarint(reader); err != nil {
			return err
		}

		if b.Side.CumulativeDifficulty.Hi, err = binary.ReadUvarint(reader); err != nil {
			return err
		}
	}

	return nil
}
