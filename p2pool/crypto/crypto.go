package crypto

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/crypto"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"unsafe"
)

func CalculateTransactionPrivateKeySeed(main, side []byte) types.Hash {
	return crypto.PooledKeccak256(
		// domain
		[]byte("tx_key_seed"), //TODO: check for null termination
		main,
		side,
	)
}

func GetDeterministicTransactionPrivateKey(seed types.Hash, previousMoneroId types.Hash) crypto.PrivateKey {
	/*
		Current deterministic key issues
		* This Deterministic private key changes too ofter, and does not fit full purpose (prevent knowledge of private keys on Coinbase without observing of sidechains).
		* It is shared across same miners on different p2pool sidechains, it does not contain Consensus Id.
		* It depends on weak sidechain historic data, but you can obtain public keys in other means.
		* A large cache must be kept containing entries for each miner in PPLNS window, for each Coinbase output. This cache is wiped when a new Monero block is found.
		* A counter is increased until the resulting hash fits the rules on deterministic scalar generation.
		* An external observer (who only observes the Monero chain) can guess the coinbase private key if they have a list of probable P2Pool miners, across all sidechains.

		k = H("tx_secret_key" | SpendPublicKey | PreviousMoneroId | uint32[counter])

	*/

	var entropy [13 + types.HashSize + types.HashSize + (int(unsafe.Sizeof(uint32(0))) /*pre-allocate uint32 for counter*/)]byte
	copy(entropy[:], []byte("tx_secret_key")) //domain
	copy(entropy[13:], seed[:])
	copy(entropy[13+types.HashSize:], previousMoneroId[:])
	return crypto.PrivateKeyFromScalar(crypto.DeterministicScalar(entropy[:13+types.HashSize+types.HashSize]))

	/*

		TODO Suggest upstream:
		* Depend on previous Monero block id. This makes it change whenever a new block is found. (already done)
		* Depend on consensus id, or an element that depends on consensus id. This makes such keys not be shared across sidechains.
		* Depend on a rolling previous P2Pool template id. Using a randomx-like seed lookup (see randomx.SeedHeight), this id can change regularly but be able to use a smaller cache for longer.
		* Some template ids are exposed on Coinbase extra data when a block is found, introduce its cumulative difficulty, and additionally the spend public key from the selected previous template.
		* A counter is increased until the resulting hash fits the rules on deterministic scalar generation (already done)


		k = H("tx_secret_key" | SeedTemplateId | SeedTemplateCumulativeDifficulty | SeedTemplateSpendPublicKey | PreviousMoneroId | uint32[counter])

		For convenience the new data could be represented via a middle hash, to reuse the old formula:

		TemplateEntropy = H(SeedTemplateId | SeedTemplateCumulativeDifficulty | SeedTemplateSpendPublicKey)
		k = H("tx_secret_key" | TemplateEntropy | PreviousMoneroId | uint32[counter])

		* This key would change on new monero blocks, or after a new seed is selected for the sidechain.
		* An external observer (who only observes the Monero chain) can guess the coinbase private key only if they know the template ids and relevant exact cumulative difficulties on the sidechain, for past shares, and the relevant P2Pool miners.
		* This is a consensus change, so it should be introduced on next P2Pool or Monero consensus change.

	*/
}
