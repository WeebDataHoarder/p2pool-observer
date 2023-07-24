module git.gammaspectra.live/P2Pool/p2pool-observer/cmd/archivetoindex

go 1.21

replace git.gammaspectra.live/P2Pool/p2pool-observer v0.0.0 => ../../

replace git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index v0.0.0 => ../index

replace git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils v0.0.0 => ../utils

replace git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache v0.0.0 => ../../p2pool/cache

require (
	git.gammaspectra.live/P2Pool/p2pool-observer v0.0.0
	git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index v0.0.0
	git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils v0.0.0
	git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache v0.0.0
	github.com/floatdrop/lru v1.3.0
)

require (
	git.gammaspectra.live/P2Pool/edwards25519 v0.0.0-20230701100949-027561bd2a33 // indirect
	git.gammaspectra.live/P2Pool/go-monero v0.0.0-20230410011208-910450c4a523 // indirect
	git.gammaspectra.live/P2Pool/go-randomx v0.0.0-20221027085532-f46adfce03a7 // indirect
	git.gammaspectra.live/P2Pool/moneroutil v0.0.0-20230722215223-18ecc51ae61e // indirect
	git.gammaspectra.live/P2Pool/randomx-go-bindings v0.0.0-20230514082649-9c5f18cd5a71 // indirect
	git.gammaspectra.live/P2Pool/sha3 v0.0.0-20230604092430-04fe7dc6439a // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/dolthub/maphash v0.1.0 // indirect
	github.com/dolthub/swiss v0.1.0 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/holiman/uint256 v1.2.3 // indirect
	github.com/jxskiss/base62 v1.1.0 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/tmthrgd/go-hex v0.0.0-20190904060850-447a3041c3bc // indirect
	go.etcd.io/bbolt v1.3.7 // indirect
	golang.org/x/crypto v0.11.0 // indirect
	golang.org/x/sys v0.10.0 // indirect
	lukechampine.com/uint128 v1.3.0 // indirect
)
