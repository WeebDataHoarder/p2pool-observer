package sidechain

import (
	"bytes"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/slices"
	"log"
)

// BlockSaveEpochSize could be up to 256?
const BlockSaveEpochSize = 32

const (
	BlockSaveOptionTemplate                    = 1 << 0
	BlockSaveOptionDeterministicPrivateKeySeed = 1 << 1
	BlockSaveOptionDeterministicBlobs          = 1 << 2
	BlockSaveOptionUncles                      = 1 << 3

	BlockSaveFieldSizeInBits = 8

	BlockSaveOffsetAddress    = BlockSaveFieldSizeInBits
	BlockSaveOffsetMainFields = BlockSaveFieldSizeInBits * 2
)

func (c *SideChain) uncompressedBlockId(block *PoolBlock) []byte {
	templateId := block.SideTemplateId(c.Consensus())
	buf := make([]byte, 0, 4+len(templateId))
	return append([]byte("RAW\x00"), buf...)
}

func (c *SideChain) compressedBlockId(block *PoolBlock) []byte {
	templateId := block.SideTemplateId(c.Consensus())
	buf := make([]byte, 0, 4+len(templateId))
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
			//TODO: check for compressed blob existence before saving uncompressed
			blob, _ := block.MarshalBinary()

			if err := c.server.SetBlob(c.uncompressedBlockId(block), blob); err != nil {
				log.Printf("error saving %s: %s", block.SideTemplateId(c.Consensus()).String(), err.Error())
			}
			return
		}
		c.sidechainLock.RLock()
		defer c.sidechainLock.RUnlock()

		calculatedOutputs := c.calculateOutputs(block)
		calcBlob, _ := calculatedOutputs.MarshalBinary()
		blockBlob, _ := block.Main.Coinbase.Outputs.MarshalBinary()
		storeBlob := bytes.Compare(calcBlob, blockBlob) != 0

		fullBlockTemplateHeight := block.Side.Height - (block.Side.Height % BlockSaveEpochSize)

		minerAddressOffset := uint64(0)

		mainFieldsOffset := uint64(0)

		parent := c.getParent(block)

		//only store keys when not deterministic
		isDeterministicPrivateKeySeed := parent != nil && c.isPoolBlockTransactionKeyIsDeterministic(block)

		if isDeterministicPrivateKeySeed && block.ShareVersion() > ShareVersion_V1 {
			expectedSeed := parent.Side.CoinbasePrivateKeySeed
			if parent.Main.PreviousId != block.Main.PreviousId {
				expectedSeed = parent.CalculateTransactionPrivateKeySeed()
			}
			if block.Side.CoinbasePrivateKeySeed != expectedSeed {
				isDeterministicPrivateKeySeed = false
			}
		}

		blob := make([]byte, 0, 4096*2)

		var blockFlags uint64

		if isDeterministicPrivateKeySeed {
			blockFlags |= BlockSaveOptionDeterministicPrivateKeySeed
		}

		if !storeBlob {
			blockFlags |= BlockSaveOptionDeterministicBlobs
		}

		transactionOffsets := make([]uint64, len(block.Main.Transactions))
		transactionOffsetsStored := 0

		parentTransactions := make([]types.Hash, 0, 512)

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

				if transactionOffsetsStored != len(transactionOffsets) {
					//store last offset to not spend time looking on already checked sections
					prevLen := len(parentTransactions)
					for _, txHash := range tmp.Main.Transactions {
						//if it doesn't exist yet
						if slices.Index(parentTransactions, txHash) == -1 {
							parentTransactions = append(parentTransactions, txHash)
						}
					}

					for tIndex, tOffset := range transactionOffsets {
						if tOffset == 0 {
							if foundIndex := slices.Index(parentTransactions[prevLen:], block.Main.Transactions[tIndex]); foundIndex != -1 {
								transactionOffsets[tIndex] = uint64(prevLen) + uint64(foundIndex) + 1
								transactionOffsetsStored++
							}
						}
					}
				}

				// early exit
				if tmp.Side.Height == fullBlockTemplateHeight || (transactionOffsetsStored == len(transactionOffsets) && mainFieldsOffset != 0 && minerAddressOffset != 0) {
					break
				}
				tmp = c.getParent(tmp)
			}
		}

		if parent == nil || block.Side.Height == fullBlockTemplateHeight { //store full blocks every once in a while, or when there is no parent block
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
		if (blockFlags&BlockSaveOffsetAddress) == 0 || (blockFlags&BlockSaveOptionTemplate) != 0 {
			blob = append(blob, block.Side.PublicSpendKey[:]...)
			blob = append(blob, block.Side.PublicViewKey[:]...)
		} else {
			blob = binary.AppendUvarint(blob, minerAddressOffset)
		}

		// private key seed, if needed
		if (blockFlags&BlockSaveOptionDeterministicPrivateKeySeed) == 0 || (block.ShareVersion() > ShareVersion_V1 && (blockFlags&BlockSaveOptionTemplate) != 0) {
			blob = append(blob, block.Side.CoinbasePrivateKeySeed[:]...)
			//public may be needed on invalid - TODO check
			//blob = append(blob, block.CoinbaseExtra(SideCoinbasePublicKey)...)
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
			blob = binary.AppendVarint(blob, int64(block.Side.Difficulty.Lo)-int64(parent.Side.Difficulty.Lo))
		}

		// main data
		// header
		if (blockFlags&BlockSaveOffsetMainFields) == 0 || (blockFlags&BlockSaveOptionTemplate) != 0 {
			blob = append(blob, block.Main.MajorVersion)
			blob = append(blob, block.Main.MinorVersion)
			//timestamp is used as difference only
			blob = binary.AppendUvarint(blob, block.Main.Timestamp)
			blob = append(blob, block.Main.PreviousId[:]...)
			blob = binary.LittleEndian.AppendUint32(blob, block.Main.Nonce)
		} else {
			blob = binary.AppendUvarint(blob, mainFieldsOffset)
			//store signed difference
			blob = binary.AppendVarint(blob, int64(block.Main.Timestamp)-int64(parent.Main.Timestamp))
			blob = binary.LittleEndian.AppendUint32(blob, block.Main.Nonce)
		}

		// coinbase
		if (blockFlags&BlockSaveOffsetMainFields) == 0 || (blockFlags&BlockSaveOptionTemplate) != 0 {
			blob = append(blob, block.Main.Coinbase.Version)
			blob = binary.AppendUvarint(blob, block.Main.Coinbase.UnlockTime)
			blob = binary.AppendUvarint(blob, block.Main.Coinbase.GenHeight)
			blob = binary.AppendUvarint(blob, block.Main.Coinbase.TotalReward-monero.TailEmissionReward)
			blob = binary.AppendUvarint(blob, uint64(len(block.CoinbaseExtra(SideExtraNonce))))
			blob = append(blob, block.CoinbaseExtra(SideExtraNonce)...)
		} else {
			blob = binary.AppendUvarint(blob, mainFieldsOffset)
			//store signed difference with parent, not template
			blob = binary.AppendVarint(blob, int64(block.Main.Timestamp)-int64(parent.Main.Timestamp))
			blob = binary.LittleEndian.AppendUint32(blob, block.Main.Nonce)
			blob = binary.AppendVarint(blob, int64(block.Main.Coinbase.TotalReward)-int64(parent.Main.Coinbase.TotalReward))
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

		fullBlob, _ := block.MarshalBinary()
		prunedBlob, _ := block.MarshalBinaryFlags(true, false)
		compactBlob, _ := block.MarshalBinaryFlags(true, true)

		if (blockFlags & BlockSaveOptionTemplate) != 0 {
			log.Printf("compress block (template) %s in compressed %d bytes, full %d bytes, pruned %d bytes, compact %d bytes", block.SideTemplateId(c.Consensus()).String(), len(blob), len(fullBlob), len(prunedBlob), len(compactBlob))
		} else {
			log.Printf("compress block %s in compressed %d bytes, full %d bytes, pruned %d bytes, compact %d bytes", block.SideTemplateId(c.Consensus()).String(), len(blob), len(fullBlob), len(prunedBlob), len(compactBlob))
		}

		if err := c.server.SetBlob(c.compressedBlockId(block), blob); err != nil {
			log.Printf("error saving %s: %s", block.SideTemplateId(c.Consensus()).String(), err.Error())
		}

	}()
}
