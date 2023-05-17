package sidechain

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"io"
)

type SideData struct {
	PublicSpendKey         crypto.PublicKeyBytes
	PublicViewKey          crypto.PublicKeyBytes
	CoinbasePrivateKeySeed types.Hash
	// CoinbasePrivateKey filled or calculated on decoding
	CoinbasePrivateKey   crypto.PrivateKeyBytes
	Parent               types.Hash
	Uncles               []types.Hash
	Height               uint64
	Difficulty           types.Difficulty
	CumulativeDifficulty types.Difficulty

	// ExtraBuffer available in ShareVersion ShareVersion_2 and above
	ExtraBuffer struct {
		SoftwareId          p2pooltypes.SoftwareId
		SoftwareVersion     p2pooltypes.SoftwareVersion
		RandomNumber        uint32
		SideChainExtraNonce uint32
	}
}

type readerAndByteReader interface {
	io.Reader
	io.ByteReader
}

func (b *SideData) BufferLength() int {
	return crypto.PublicKeySize + crypto.PublicKeySize + types.HashSize + types.HashSize + binary.MaxVarintLen64 + len(b.Uncles)*types.HashSize + binary.MaxVarintLen64 + binary.MaxVarintLen64 + binary.MaxVarintLen64 + binary.MaxVarintLen64 + binary.MaxVarintLen64 + 4*4
}

func (b *SideData) MarshalBinary(version ShareVersion) (buf []byte, err error) {
	buf = make([]byte, 0, b.BufferLength())
	buf = append(buf, b.PublicSpendKey[:]...)
	buf = append(buf, b.PublicViewKey[:]...)
	if version > ShareVersion_V1 {
		buf = append(buf, b.CoinbasePrivateKeySeed[:]...)
	} else {
		buf = append(buf, b.CoinbasePrivateKey[:]...)
	}
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
	if version > ShareVersion_V1 {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(b.ExtraBuffer.SoftwareId))
		buf = binary.LittleEndian.AppendUint32(buf, uint32(b.ExtraBuffer.SoftwareVersion))
		buf = binary.LittleEndian.AppendUint32(buf, b.ExtraBuffer.RandomNumber)
		buf = binary.LittleEndian.AppendUint32(buf, b.ExtraBuffer.SideChainExtraNonce)
	}

	return buf, nil
}

func (b *SideData) FromReader(reader readerAndByteReader, version ShareVersion) (err error) {
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
	if version > ShareVersion_V1 {
		//needs preprocessing
		if _, err = io.ReadFull(reader, b.CoinbasePrivateKeySeed[:]); err != nil {
			return err
		}
	} else {
		if _, err = io.ReadFull(reader, b.CoinbasePrivateKey[:]); err != nil {
			return err
		}
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
	if version > ShareVersion_V1 {
		if err = binary.Read(reader, binary.LittleEndian, &b.ExtraBuffer.SoftwareId); err != nil {
			return fmt.Errorf("within extra buffer: %w", err)
		}
		if err = binary.Read(reader, binary.LittleEndian, &b.ExtraBuffer.SoftwareVersion); err != nil {
			return fmt.Errorf("within extra buffer: %w", err)
		}
		if err = binary.Read(reader, binary.LittleEndian, &b.ExtraBuffer.RandomNumber); err != nil {
			return fmt.Errorf("within extra buffer: %w", err)
		}
		if err = binary.Read(reader, binary.LittleEndian, &b.ExtraBuffer.SideChainExtraNonce); err != nil {
			return fmt.Errorf("within extra buffer: %w", err)
		}
	}

	return nil
}

func (b *SideData) UnmarshalBinary(data []byte, version ShareVersion) error {
	reader := bytes.NewReader(data)
	return b.FromReader(reader, version)
}
