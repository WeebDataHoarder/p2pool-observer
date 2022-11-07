package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"sync/atomic"
	"unsafe"
)

const HandshakeChallengeSize = 8
const HandshakeChallengeDifficulty = 10000

type HandshakeChallenge [HandshakeChallengeSize]byte

func FindChallengeSolution(challenge HandshakeChallenge, consensusId types.Hash, stop *atomic.Bool) (solution uint64, hash types.Hash, ok bool) {

	var buf [HandshakeChallengeSize*2 + types.HashSize]byte
	copy(buf[:], challenge[:])
	copy(buf[HandshakeChallengeSize:], consensusId[:])
	var salt uint64

	var saltSlice [int(unsafe.Sizeof(salt))]byte
	_, _ = rand.Read(saltSlice[:])
	salt = binary.LittleEndian.Uint64(saltSlice[:])

	h := crypto.GetKeccak256Hasher()
	defer crypto.PutKeccak256Hasher(h)

	var sum types.Hash

	for {
		h.Reset()
		binary.LittleEndian.PutUint64(buf[types.HashSize+HandshakeChallengeSize:], salt)
		_, _ = h.Write(buf[:])
		crypto.HashFastSum(h, sum[:])

		//check if we have been asked to stop
		if stop.Load() {
			return salt, sum, false
		}

		if types.DifficultyFrom64(binary.LittleEndian.Uint64(sum[len(sum)-int(unsafe.Sizeof(uint64(0))):])).Mul64(HandshakeChallengeDifficulty).Hi == 0 {
			//found solution
			return salt, sum, true
		}

		salt++
	}
}

func CalculateChallengeHash(challenge HandshakeChallenge, consensusId types.Hash, solution uint64) (hash types.Hash, ok bool) {
	hash = crypto.PooledKeccak256(challenge[:], consensusId[:], binary.LittleEndian.AppendUint64(nil, solution))
	return hash, types.DifficultyFrom64(binary.LittleEndian.Uint64(hash[types.HashSize-int(unsafe.Sizeof(uint64(0))):])).Mul64(HandshakeChallengeDifficulty).Hi == 0
}
