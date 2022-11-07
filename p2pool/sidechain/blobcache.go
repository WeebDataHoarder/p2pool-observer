package sidechain

import (
	"bytes"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"golang.org/x/exp/slices"
	"log"
)


// BlockSaveEpochSize could be up to 256?
const BlockSaveEpochSize = 32

const (
	BlockSaveOptionTemplate                = 1 << 0
	BlockSaveOptionDeterministicPrivateKey = 1 << 1
	BlockSaveOptionDeterministicBlobs = 1 << 2
	BlockSaveOptionUncles = 1 << 3

	BlockSaveFieldSizeInBits = 8

	BlockSaveOffsetAddress = BlockSaveFieldSizeInBits
	BlockSaveOffsetMainFields = BlockSaveFieldSizeInBits * 2

)

func (c *SideChain) uncompressedBlockId(block *PoolBlock) []byte {
	templateId := block.SideTemplateId(c.Consensus())
	buf := make([]byte, 0, 4 + len(templateId))
	return append([]byte("RAW\x00"), buf...)
}

func (c *SideChain) compressedBlockId(block *PoolBlock) []byte {
	templateId := block.SideTemplateId(c.Consensus())
	buf := make([]byte, 0, 4 + len(templateId))
	return append([]byte("PAK\x00"), buf...)
}

func (c *SideChain) saveBlock(block *PoolBlock) {
	go func() {
		//TODO: make this a worker with a queue?

		if !block.Verified.Load() || block.Invalid.Load() {
			blob, _ := block.MarshalBinary()

			if err := c.server.SetBlob(c.uncompressedBlockId(block), blob); err != nil {
				log.Printf("error saving %s: %s", block.SideTemplateId(c.Consensus()).String(), err.Error())
			}
			return
		}
		if block.Depth.Load() >= c.Consensus().ChainWindowSize {
			//TODO: save raw
			return
		}
		c.sidechainLock.RLock()
		defer c.sidechainLock.RUnlock()

		//only store keys when not deterministic
		storePrivateTransactionKey := !c.isPoolBlockTransactionKeyIsDeterministic(block)

		calculatedOutputs := c.calculateOutputs(block)
		calcBlob, _ := calculatedOutputs.MarshalBinary()
		blockBlob, _ := block.Main.Coinbase.Outputs.MarshalBinary()
		storeBlob := bytes.Compare(calcBlob, blockBlob) != 0

		fullBlockTemplateHeight := block.Side.Height - (block.Side.Height % BlockSaveEpochSize)
		var templateBlock *PoolBlock

		minerAddressOffset := uint64(0)

		mainFieldsOffset := uint64(0)

		parent := c.getParent(block)

		blob := make([]byte, 0, 4096*2)

		var blockFlags uint64

		if !storePrivateTransactionKey {
			blockFlags |= BlockSaveOptionDeterministicPrivateKey
		}

		if !storeBlob {
			blockFlags |= BlockSaveOptionDeterministicBlobs
		}

		transactionOffsets := make([]uint64, len(block.Main.Transactions))

		if block.Side.Height != fullBlockTemplateHeight {
			tmp := parent
			for offset := uint64(1); tmp != nil && offset < BlockSaveEpochSize; offset++ {
				if !tmp.Verified.Load() || tmp.Invalid.Load() {
					break
				}

				if minerAddressOffset == 0 && tmp.Side.PublicSpendKey == block.Side.PublicSpendKey && tmp.Side.PublicViewKey == block.Side.PublicViewKey {
					minerAddressOffset = block.Side.Height - tmp.Side.Height
				}

				if mainFieldsOffset == 0 && tmp.Main.Coinbase.Version == block.Main.Coinbase.Version && tmp.Main.Coinbase.UnlockTime == block.Main.Coinbase.UnlockTime && tmp.Main.Coinbase.GenHeight == block.Main.Coinbase.GenHeight && tmp.Main.PreviousId == block.Main.PreviousId && tmp.Main.MajorVersion == block.Main.MajorVersion && tmp.Main.MinorVersion == block.Main.MinorVersion {
					mainFieldsOffset = block.Side.Height - tmp.Side.Height
				}

				for tIndex, tOffset := range transactionOffsets {
					if tOffset == 0 {
						if foundIndex := slices.Index(tmp.Main.Transactions, block.Main.Transactions[tIndex]); foundIndex != -1 {
							transactionOffsets[tIndex] = (uint64(foundIndex) << BlockSaveFieldSizeInBits) | ((block.Side.Height - tmp.Side.Height) & 0xFF)
						}
					}
				}

				if tmp.Side.Height == fullBlockTemplateHeight {
					templateBlock = tmp
					break
				}
				tmp = c.getParent(tmp)
			}
		}



		if parent == nil || templateBlock == nil || (block.Side.Height % BlockSaveEpochSize) == 0 { //store full blocks every once in a while, or when there is no template block
			blockFlags |= BlockSaveOptionTemplate
		} else {
			if minerAddressOffset > 0 {
				blockFlags |= minerAddressOffset << BlockSaveOffsetAddress
			}
			if mainFieldsOffset > 0 {
				blockFlags |= mainFieldsOffset << BlockSaveOffsetMainFields
			}
		}

		if len(block.Side.Uncles) > 0 {
			blockFlags |= BlockSaveOptionUncles
		}


		blob = binary.AppendUvarint(blob, blockFlags)

		// side data

		// miner address
		if (blockFlags & BlockSaveOffsetAddress) == 0 || (blockFlags & BlockSaveOptionTemplate) != 0 {
			blob = append(blob, block.Side.PublicSpendKey[:]...)
			blob = append(blob, block.Side.PublicViewKey[:]...)
		} else {
			blob = binary.AppendUvarint(blob, minerAddressOffset)
		}

		// private key, if needed
		if (blockFlags & BlockSaveOptionDeterministicPrivateKey) == 0 {
			blob = append(blob, block.Side.CoinbasePrivateKey[:]...)
		}

		// parent
		blob = append(blob, block.Side.Parent[:]...)

		// uncles
		if (blockFlags & BlockSaveOptionUncles) > 0 {
			blob = binary.AppendUvarint(blob, uint64(len(block.Side.Uncles)))
			for _, uncleId := range block.Side.Uncles {
				blob = append(blob, uncleId[:]...)
			}
		}

		//no height saved except on templates
		if (blockFlags & BlockSaveOptionTemplate) != 0 {
			blob = binary.AppendUvarint(blob, block.Side.Height)
		}

		//difficulty
		if (blockFlags & BlockSaveOptionTemplate) != 0 {
			blob = binary.AppendUvarint(blob, block.Side.Difficulty.Lo)
			blob = binary.AppendUvarint(blob, block.Side.CumulativeDifficulty.Lo)
			blob = binary.AppendUvarint(blob, block.Side.CumulativeDifficulty.Hi)
		} else {
			//store signed difference
			blob = binary.AppendVarint(blob, int64(block.Side.Difficulty.Lo) - int64(parent.Side.Difficulty.Lo))
		}

		// main data
		// header
		if (blockFlags & BlockSaveOffsetMainFields) == 0 || (blockFlags & BlockSaveOptionTemplate) != 0 {
			blob = append(blob, block.Main.MajorVersion)
			blob = append(blob, block.Main.MinorVersion)
			//timestamp is used as difference only
			blob = binary.AppendUvarint(blob, block.Main.Timestamp)
			blob = append(blob, block.Main.PreviousId[:]...)
			blob = binary.LittleEndian.AppendUint32(blob, block.Main.Nonce)
		} else {
			blob = binary.AppendUvarint(blob, mainFieldsOffset)
			//store signed difference
			blob = binary.AppendVarint(blob, int64(block.Main.Timestamp) - int64(templateBlock.Main.Timestamp))
			blob = binary.LittleEndian.AppendUint32(blob, block.Main.Nonce)
		}

		// coinbase
		if (blockFlags & BlockSaveOffsetMainFields) == 0 || (blockFlags & BlockSaveOptionTemplate) != 0 {
			blob = append(blob, block.Main.Coinbase.Version)
			blob = binary.AppendUvarint(blob, block.Main.Coinbase.UnlockTime)
			blob = binary.AppendUvarint(blob, block.Main.Coinbase.GenHeight)
			blob = binary.AppendUvarint(blob, block.Main.Coinbase.TotalReward - monero.TailEmissionReward)
			blob = binary.AppendUvarint(blob, uint64(len(block.CoinbaseExtra(SideExtraNonce))))
			blob = append(blob, block.CoinbaseExtra(SideExtraNonce)...)
		} else {
			blob = binary.AppendUvarint(blob, mainFieldsOffset)
			//store signed difference with parent, not template
			blob = binary.AppendVarint(blob, int64(block.Main.Timestamp) - int64(parent.Main.Timestamp))
			blob = binary.LittleEndian.AppendUint32(blob, block.Main.Nonce)
			blob = binary.AppendVarint(blob, int64(block.Main.Coinbase.TotalReward) - int64(parent.Main.Coinbase.TotalReward))
			blob = binary.AppendUvarint(blob, uint64(len(block.CoinbaseExtra(SideExtraNonce))))
			blob = append(blob, block.CoinbaseExtra(SideExtraNonce)...)
		}

		// coinbase blob, if needed
		if (blockFlags & BlockSaveOptionDeterministicBlobs) == 0 {
			blob = append(blob, blockBlob...)
		}


		//transactions
		if (blockFlags & BlockSaveOptionTemplate) != 0 {
			blob = binary.AppendUvarint(blob, uint64(len(block.Main.Transactions)))
			for _, txId := range block.Main.Transactions {
				blob = append(blob, txId[:]...)
			}
		} else {
			blob = binary.AppendUvarint(blob, uint64(len(block.Main.Transactions)))
			for i, v := range transactionOffsets {
				blob = binary.AppendUvarint(blob, v)
				if v == 0 {
					blob = append(blob, block.Main.Transactions[i][:]...)
				}
			}
		}

		bblob, _ := block.MarshalBinary()
		log.Printf("compress block %s in compressed %d bytes, full %d bytes, pruned %d bytes", block.SideTemplateId(c.Consensus()).String(), len(blob), len(bblob), len(bblob) - len(blockBlob))

		if err := c.server.SetBlob(c.compressedBlockId(block), blob); err != nil {
			log.Printf("error saving %s: %s", block.SideTemplateId(c.Consensus()).String(), err.Error())
		}

	}()
}