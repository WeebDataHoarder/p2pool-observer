package main

import (
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	p2poolapi "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/api"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"time"
)

func main() {
	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	startFromHeight := flag.Uint64("from", 0, "Start sweep from this main height")
	dbString := flag.String("db", "", "")
	p2poolApiHost := flag.String("api-host", "", "Host URL for p2pool go observer consensus")
	flag.Parse()

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	client.GetDefaultClient().SetThrottle(1000)

	p2api := p2poolapi.NewP2PoolApi(*p2poolApiHost)

	if err := p2api.WaitSync(); err != nil {
		utils.Panic(err)
	}

	indexDb, err := index.OpenIndex(*dbString, p2api.Consensus(), p2api.DifficultyByHeight, p2api.SeedByHeight, p2api.ByTemplateId)
	if err != nil {
		utils.Panic(err)
	}
	defer indexDb.Close()

	startHeight := *startFromHeight

	var tipHeight uint64

	if err := indexDb.Query("SELECT MIN(height), MAX(height) FROM main_blocks WHERE height > 0;", func(row index.RowScanInterface) error {
		var minHeight uint64
		if err := row.Scan(&minHeight, &tipHeight); err != nil {
			return err
		}

		if minHeight > startHeight {
			startHeight = minHeight
		}
		return nil
	}); err != nil {
		utils.Panic(err)
	}

	stopHeight := tipHeight - monero.MinerRewardUnlockTime
	if tipHeight < monero.MinerRewardUnlockTime {
		stopHeight = tipHeight
	}

	utils.Logf("", "Starting at height %d, stopping at %d", startHeight, stopHeight)

	isProcessedPrevious := true
	for height := startHeight; height <= stopHeight; height++ {
		b := indexDb.GetMainBlockByHeight(height)
		if b == nil {
			utils.Logf("", "Block at %d is nil", height)
			continue
		}

		if isProcessed, ok := b.GetMetadata("processed").(bool); ok && isProcessed && isProcessedPrevious {
			utils.Logf("", "Block %s at %d (%s) has already been processed", b.Id, b.Height, time.Unix(int64(b.Timestamp), 0).UTC().Format("02-01-2006 15:04:05 MST"))
			continue
		}

		isProcessedPrevious = false

		if err := cmdutils.ProcessFullBlock(b, indexDb); err != nil {
			utils.Logf("", "error processing block %s at %d: %s", b.Id, b.Height, err)
		}
	}

}
