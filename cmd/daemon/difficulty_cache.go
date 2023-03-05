package main

import (
	"context"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/rand"
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

// TODO: remove
func cacheHeightDifficulty(height uint64) {
	if _, ok := getHeightDifficulty(height); !ok {
		if header, err := client.GetDefaultClient().GetBlockHeaderByHeight(height, context.Background()); err != nil {
			if template, err := client.GetDefaultClient().GetBlockTemplate(types.DonationAddress); err != nil {
				setHeightDifficulty(uint64(template.Height), types.DifficultyFrom64(uint64(template.Difficulty)))
			}
		} else {
			setHeightDifficulty(header.BlockHeader.Height, types.DifficultyFrom64(header.BlockHeader.Difficulty))
		}
	}
}
