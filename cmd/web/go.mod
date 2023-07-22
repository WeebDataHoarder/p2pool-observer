module git.gammaspectra.live/P2Pool/p2pool-observer/cmd/web

go 1.21

replace git.gammaspectra.live/P2Pool/p2pool-observer v0.0.0 => ../../

replace git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index v0.0.0 => ../index

replace git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils v0.0.0 => ../utils

require (
	git.gammaspectra.live/P2Pool/p2pool-observer v0.0.0
	git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index v0.0.0
	git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils v0.0.0
	github.com/ake-persson/mapslice-json v0.0.0-20210720081907-22c8edf57807
	github.com/gorilla/mux v1.8.0
	github.com/valyala/quicktemplate v1.7.0
)

require (
	git.gammaspectra.live/P2Pool/edwards25519 v0.0.0-20230701100949-027561bd2a33 // indirect
	git.gammaspectra.live/P2Pool/go-monero v0.0.0-20230410011208-910450c4a523 // indirect
	git.gammaspectra.live/P2Pool/go-randomx v0.0.0-20221027085532-f46adfce03a7 // indirect
	git.gammaspectra.live/P2Pool/moneroutil v0.0.0-20230527152251-7b24ed2d11ce // indirect
	git.gammaspectra.live/P2Pool/randomx-go-bindings v0.0.0-20230514082649-9c5f18cd5a71 // indirect
	git.gammaspectra.live/P2Pool/sha3 v0.0.0-20230604092430-04fe7dc6439a // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/dolthub/maphash v0.1.0 // indirect
	github.com/dolthub/swiss v0.1.0 // indirect
	github.com/floatdrop/lru v1.3.0 // indirect
	github.com/goccy/go-json v0.10.2 // indirect
	github.com/holiman/uint256 v1.2.3 // indirect
	github.com/jxskiss/base62 v1.1.0 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	golang.org/x/crypto v0.11.0 // indirect
	golang.org/x/sys v0.10.0 // indirect
	lukechampine.com/uint128 v1.3.0 // indirect
)
