package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/rand"
	"lukechampine.com/uint128"
	"sync"
)

var difficultyCache = make(map[uint64]types.Difficulty)
var difficultyCacheLock sync.RWMutex

func getHeightDifficulty(height uint64) (difficulty types.Difficulty, ok bool) {
	difficultyCacheLock.RLock()
	defer difficultyCacheLock.RUnlock()
	difficulty, ok = difficultyCache[height]
	return
}

func setHeightDifficulty(height uint64, difficulty types.Difficulty) {
	difficultyCacheLock.Lock()
	defer difficultyCacheLock.Unlock()

	if len(difficultyCache) >= 1024 {
		//Delete key at random
		//TODO: FIFO
		keys := maps.Keys(difficultyCache)
		delete(difficultyCache, keys[rand.Intn(len(keys))])
	}

	difficultyCache[height] = difficulty
}

func cacheHeightDifficulty(height uint64) {
	if _, ok := getHeightDifficulty(height); !ok {
		if header, err := client.GetClient().GetBlockHeaderByHeight(height); err != nil {
			if template, err := client.GetClient().GetBlockTemplate(types.DonationAddress); err != nil {
				setHeightDifficulty(uint64(template.Height), types.Difficulty{Uint128: uint128.From64(uint64(template.Difficulty))})
			}
		} else {
			setHeightDifficulty(header.BlockHeader.Height, types.Difficulty{Uint128: uint128.From64(header.BlockHeader.Difficulty)})
		}
	}
}
