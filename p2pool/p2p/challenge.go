package p2p

import (
	"crypto/rand"
	"encoding/binary"
	"git.gammaspectra.live/P2Pool/moneroutil"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/crypto/sha3"
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

	h := sha3.NewLegacyKeccak256()

	sum := make([]byte, types.HashSize)

	for {
		h.Reset()
		binary.LittleEndian.PutUint64(buf[types.HashSize+HandshakeChallengeSize:], salt)
		_, _ = h.Write(buf[:])
		sum = h.Sum(sum[:0])

		//check if we have been asked to stop
		if stop.Load() {
			return salt, types.HashFromBytes(sum), false
		}

		if types.DifficultyFrom64(binary.LittleEndian.Uint64(sum[len(sum)-int(unsafe.Sizeof(uint64(0))):])).Mul64(HandshakeChallengeDifficulty).Hi == 0 {
			//found solution
			return salt, types.HashFromBytes(sum), true
		}

		salt++
	}
}

func CalculateChallengeHash(challenge HandshakeChallenge, consensusId types.Hash, solution uint64) (hash types.Hash, ok bool) {
	hash = types.Hash(moneroutil.Keccak256(challenge[:], consensusId[:], binary.LittleEndian.AppendUint64(nil, solution)))
	return hash, types.DifficultyFrom64(binary.LittleEndian.Uint64(hash[types.HashSize-int(unsafe.Sizeof(uint64(0))):])).Mul64(HandshakeChallengeDifficulty).Hi == 0
}
