package crypto

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"git.gammaspectra.live/P2Pool/sha3"
)

type BinaryTreeHash []types.Hash

func (t BinaryTreeHash) leafHash(hasher *sha3.HasherState) (rootHash types.Hash) {
	switch len(t) {
	case 0:
		panic("unsupported length")
	case 1:
		return t[0]
	case 2:
		hasher.Reset()
		_, _ = hasher.Write(t[0][:])
		_, _ = hasher.Write(t[1][:])
		HashFastSum(hasher, rootHash[:])
		return rootHash
	default:
		panic("unsupported length")
	}
}

func (t BinaryTreeHash) RootHash() (rootHash types.Hash) {

	hasher := GetKeccak256Hasher()
	defer PutKeccak256Hasher(hasher)
	count := len(t)
	if count <= 2 {
		return t.leafHash(hasher)
	}

	cnt := utils.PreviousPowerOfTwo(uint64(len(t)))

	temporaryTree := make(BinaryTreeHash, cnt)
	copy(temporaryTree, t[:cnt*2-count])

	offset := cnt*2 - count
	for i := 0; (i + offset) < cnt; i++ {
		temporaryTree[offset+i] = t[offset+i*2 : offset+i*2+2].leafHash(hasher)
	}

	for cnt > 2 {
		cnt >>= 1
		for i := 0; i < cnt; i++ {
			temporaryTree[i] = temporaryTree[i*2 : i*2+2].leafHash(hasher)
		}
	}

	rootHash = temporaryTree[:2].leafHash(hasher)

	return
}
