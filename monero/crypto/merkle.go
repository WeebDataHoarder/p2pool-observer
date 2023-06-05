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
	default:
		//only hash the next two items
		hasher.Reset()
		_, _ = hasher.Write(t[0][:])
		_, _ = hasher.Write(t[1][:])
		HashFastSum(hasher, rootHash[:])
		return rootHash
	}
}

func (t BinaryTreeHash) RootHash() (rootHash types.Hash) {

	hasher := GetKeccak256Hasher()
	defer PutKeccak256Hasher(hasher)
	count := len(t)
	if count <= 2 {
		return t.leafHash(hasher)
	}

	pow2cnt := utils.PreviousPowerOfTwo(uint64(count))
	offset := pow2cnt*2 - count

	temporaryTree := make(BinaryTreeHash, pow2cnt)
	copy(temporaryTree, t[:offset])

	offsetTree := temporaryTree[offset:]
	for i := range offsetTree {
		offsetTree[i] = t[offset+i*2:].leafHash(hasher)
	}

	for pow2cnt >>= 1; pow2cnt > 1; pow2cnt >>= 1 {
		for i := range temporaryTree[:pow2cnt] {
			temporaryTree[i] = temporaryTree[i*2:].leafHash(hasher)
		}
	}

	rootHash = temporaryTree.leafHash(hasher)

	return
}

func (t BinaryTreeHash) MainBranch() (mainBranch []types.Hash) {

	hasher := GetKeccak256Hasher()
	defer PutKeccak256Hasher(hasher)
	count := len(t)
	if count <= 2 {
		return nil
	}

	pow2cnt := utils.PreviousPowerOfTwo(uint64(count))
	offset := pow2cnt*2 - count

	temporaryTree := make(BinaryTreeHash, pow2cnt)
	copy(temporaryTree, t[:offset])

	offsetTree := temporaryTree[offset:]

	for i := range offsetTree {
		if (offset + i*2) == 0 {
			mainBranch = append(mainBranch, t[1])
		}
		offsetTree[i] = t[offset+i*2:].leafHash(hasher)
	}

	for pow2cnt >>= 1; pow2cnt > 1; pow2cnt >>= 1 {
		for i := range temporaryTree[:pow2cnt] {
			if i == 0 {
				mainBranch = append(mainBranch, temporaryTree[1])
			}

			temporaryTree[i] = temporaryTree[i*2:].leafHash(hasher)
		}
	}

	mainBranch = append(mainBranch, temporaryTree[1])

	return
}
