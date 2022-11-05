package sidechain

import (
	"bytes"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
)

type SideData struct {
	PublicSpendKey       crypto.PublicKeyBytes
	PublicViewKey        crypto.PublicKeyBytes
	CoinbasePrivateKey   crypto.PrivateKeyBytes
	Parent               types.Hash
	Uncles               []types.Hash
	Height               uint64
	Difficulty           types.Difficulty
	CumulativeDifficulty types.Difficulty
}

type readerAndByteReader interface {
	io.Reader
	io.ByteReader
}

func (b *SideData) MarshalBinary() (buf []byte, err error) {
	buf = make([]byte, 0, types.HashSize+types.HashSize+types.HashSize+types.HashSize+binary.MaxVarintLen64+binary.MaxVarintLen64+binary.MaxVarintLen64+binary.MaxVarintLen64+binary.MaxVarintLen64+binary.MaxVarintLen64)
	buf = append(buf, b.PublicSpendKey[:]...)
	buf = append(buf, b.PublicViewKey[:]...)
	buf = append(buf, b.CoinbasePrivateKey[:]...)
	buf = append(buf, b.Parent[:]...)
	buf = binary.AppendUvarint(buf, uint64(len(b.Uncles)))
	for _, uId := range b.Uncles {
		buf = append(buf, uId[:]...)
	}
	buf = binary.AppendUvarint(buf, b.Height)
	buf = binary.AppendUvarint(buf, b.Difficulty.Lo)
	buf = binary.AppendUvarint(buf, b.Difficulty.Hi)
	buf = binary.AppendUvarint(buf, b.CumulativeDifficulty.Lo)
	buf = binary.AppendUvarint(buf, b.CumulativeDifficulty.Hi)

	return buf, nil
}

func (b *SideData) FromReader(reader readerAndByteReader) (err error) {
	var (
		uncleCount uint64
		uncleHash  types.Hash
	)
	if _, err = io.ReadFull(reader, b.PublicSpendKey[:]); err != nil {
		return err
	}
	if _, err = io.ReadFull(reader, b.PublicViewKey[:]); err != nil {
		return err
	}
	if _, err = io.ReadFull(reader, b.CoinbasePrivateKey[:]); err != nil {
		return err
	}
	if _, err = io.ReadFull(reader, b.Parent[:]); err != nil {
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
		b.Uncles = append(b.Uncles, uncleHash)
	}

	if b.Height, err = binary.ReadUvarint(reader); err != nil {
		return err
	}

	{
		if b.Difficulty.Lo, err = binary.ReadUvarint(reader); err != nil {
			return err
		}

		if b.Difficulty.Hi, err = binary.ReadUvarint(reader); err != nil {
			return err
		}
	}

	{
		if b.CumulativeDifficulty.Lo, err = binary.ReadUvarint(reader); err != nil {
			return err
		}

		if b.CumulativeDifficulty.Hi, err = binary.ReadUvarint(reader); err != nil {
			return err
		}
	}

	return nil
}

func (b *SideData) UnmarshalBinary(data []byte) error {
	reader := bytes.NewReader(data)
	return b.FromReader(reader)
}
