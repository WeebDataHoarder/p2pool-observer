package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client/zmq"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/archive"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/cache/legacy"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/mainchain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/mempool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/p2p"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"log"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type EventListener struct {
	ListenerId  uint64
	Tip         func(tip *sidechain.PoolBlock)
	Broadcast   func(b *sidechain.PoolBlock)
	Found       func(data *sidechain.ChainMain, b *sidechain.PoolBlock)
	MainData    func(data *sidechain.ChainMain)
	MinerData   func(data *p2pooltypes.MinerData)
	MempoolData func(data mempool.Mempool)
}

type P2Pool struct {
	consensus *sidechain.Consensus
	sidechain *sidechain.SideChain
	mainchain *mainchain.MainChain
	archive   cache.AddressableCache
	cache     cache.HeapCache
	server    *p2p.Server

	ctx       context.Context
	ctxCancel context.CancelFunc

	rpcClient *client.Client
	zmqClient *zmq.Client

	recentSubmittedBlocks *utils.CircularBuffer[types.Hash]

	listenersLock  sync.RWMutex
	listeners      []EventListener
	nextListenerId uint64

	started    atomic.Bool
	closeError error
	closed     chan struct{}
}

// AddListener Registers listener to several events produced centrally.
// Note that you should process events called as fast as possible, or spawn a new goroutine to not block
func (p *P2Pool) AddListener(tip func(tip *sidechain.PoolBlock), broadcast func(b *sidechain.PoolBlock), found func(data *sidechain.ChainMain, b *sidechain.PoolBlock), mainData func(data *sidechain.ChainMain), minerData func(data *p2pooltypes.MinerData), mempoolData func(data mempool.Mempool)) (listenerId uint64) {
	p.listenersLock.Lock()
	p.listenersLock.Unlock()

	listenerId = p.nextListenerId
	p.nextListenerId++
	p.listeners = append(p.listeners, EventListener{
		ListenerId:  listenerId,
		Tip:         tip,
		Broadcast:   broadcast,
		Found:       found,
		MainData:    mainData,
		MinerData:   minerData,
		MempoolData: mempoolData,
	})
	return listenerId
}

func (p *P2Pool) RemoveListener(listenerId uint64) bool {
	p.listenersLock.Lock()
	p.listenersLock.Unlock()
	if i := slices.IndexFunc(p.listeners, func(listener EventListener) bool {
		return listener.ListenerId == listenerId
	}); i != -1 {
		p.listeners = slices.Delete(p.listeners, i, i+1)
		return true
	}
	return false
}

func (p *P2Pool) GetBlob(key []byte) (blob []byte, err error) {
	return nil, errors.New("not found")
}

func (p *P2Pool) SetBlob(key, blob []byte) (err error) {
	return nil
}

func (p *P2Pool) RemoveBlob(key []byte) (err error) {
	return nil
}

func (p *P2Pool) AddressableCache() cache.AddressableCache {
	return p.archive
}

func (p *P2Pool) Cache() cache.Cache {
	return p.cache
}

func (p *P2Pool) CloseError() error {
	return p.closeError
}

func (p *P2Pool) WaitUntilClosed() {
	<-p.closed
}

func (p *P2Pool) Close(err error) {
	started := p.started.Swap(false)

	if started {
		p.closeError = err
	}

	p.ctxCancel()
	_ = p.zmqClient.Close()

	if p.server != nil {
		p.server.Close()
	}
	if p.cache != nil {
		p.cache.Close()
	}
	if p.archive != nil {
		p.archive.Close()
	}

	if started && err != nil {
		close(p.closed)
		log.Panicf("[P2Pool] Closed due to error %s", err)
	} else if started {
		close(p.closed)
		log.Printf("[P2Pool] Closed")
	} else if err != nil {
		log.Printf("[P2Pool] Received extra error during closing %s", err)
	}
}

func NewP2Pool(consensus *sidechain.Consensus, settings map[string]string) (*P2Pool, error) {

	if settings["full-mode"] == "true" {
		if err := consensus.InitHasher(2, randomx.FlagSecure, randomx.FlagFullMemory); err != nil {
			return nil, err
		}
	} else {
		if err := consensus.InitHasher(2, randomx.FlagSecure); err != nil {
			return nil, err
		}
	}

	pool := &P2Pool{
		consensus:             consensus,
		recentSubmittedBlocks: utils.NewCircularBuffer[types.Hash](8),
		closed:                make(chan struct{}),
	}
	var err error

	pool.ctx, pool.ctxCancel = context.WithCancel(context.Background())

	listenAddress := fmt.Sprintf("0.0.0.0:%d", pool.Consensus().DefaultPort())

	if pool.rpcClient, err = client.NewClient(settings["rpc-url"]); err != nil {
		return nil, err
	}

	pool.zmqClient = zmq.NewClient(settings["zmq-url"], zmq.TopicFullChainMain, zmq.TopicFullMinerData, zmq.TopicMinimalTxPoolAdd)

	pool.sidechain = sidechain.NewSideChain(pool)

	pool.mainchain = mainchain.NewMainChain(pool.sidechain, pool)

	if pool.mainchain == nil {
		return nil, errors.New("could not create MainChain")
	}

	if archivePath, ok := settings["archive"]; ok {
		if pool.archive, err = archive.NewCache(archivePath, pool.consensus, pool.GetDifficultyByHeight); err != nil {
			return nil, fmt.Errorf("could not create cache: %w", err)
		}
	}

	if cachePath, ok := settings["cache"]; ok {
		if pool.cache, err = legacy.NewCache(consensus, cachePath); err != nil {
			return nil, fmt.Errorf("could not create cache: %w", err)
		}
	}

	if addr, ok := settings["listen"]; ok {
		listenAddress = addr
	}

	maxOutgoingPeers := uint64(10)
	if outgoingPeers, ok := settings["out-peers"]; ok {
		maxOutgoingPeers, _ = strconv.ParseUint(outgoingPeers, 10, 0)
	}

	maxIncomingPeers := uint64(450)
	if incomingPeers, ok := settings["in-peers"]; ok {
		maxIncomingPeers, _ = strconv.ParseUint(incomingPeers, 10, 0)
	}

	externalListenPort := uint64(0)
	if externalPort, ok := settings["external-port"]; ok {
		externalListenPort, _ = strconv.ParseUint(externalPort, 10, 0)
	}

	useIPv4, useIPv6 := true, true
	if b, ok := settings["ipv6-only"]; ok && b == "true" {
		useIPv4 = false
		useIPv6 = true
	}

	if pool.server, err = p2p.NewServer(pool, listenAddress, uint16(externalListenPort), uint32(maxOutgoingPeers), uint32(maxIncomingPeers), useIPv4, useIPv6, pool.ctx); err != nil {
		return nil, err
	}

	return pool, nil
}

func (p *P2Pool) GetChainMainByHash(hash types.Hash) *sidechain.ChainMain {
	return p.mainchain.GetChainMainByHash(hash)
}

func (p *P2Pool) GetChainMainByHeight(height uint64) *sidechain.ChainMain {
	return p.mainchain.GetChainMainByHeight(height)
}

func (p *P2Pool) GetChainMainTip() *sidechain.ChainMain {
	return p.mainchain.GetChainMainTip()
}

func (p *P2Pool) GetMinerDataTip() *p2pooltypes.MinerData {
	return p.mainchain.GetMinerDataTip()
}

// GetMinimalBlockHeaderByHeight Only Id / Height / Timestamp are assured
func (p *P2Pool) GetMinimalBlockHeaderByHeight(height uint64) *block.Header {
	lowerThanTip := height <= p.mainchain.GetChainMainTip().Height
	if chainMain := p.mainchain.GetChainMainByHeight(height); chainMain != nil && chainMain.Id != types.ZeroHash {
		prev := p.mainchain.GetChainMainByHeight(height - 1)
		if prev != nil {
			return &block.Header{
				Timestamp:  chainMain.Timestamp,
				Height:     chainMain.Height,
				Reward:     chainMain.Reward,
				Difficulty: chainMain.Difficulty,
				Id:         chainMain.Id,
				PreviousId: prev.Id,
			}
		} else {
			return &block.Header{
				Timestamp:  chainMain.Timestamp,
				Height:     chainMain.Height,
				Reward:     chainMain.Reward,
				Difficulty: chainMain.Difficulty,
				Id:         chainMain.Id,
			}
		}
	} else if lowerThanTip {
		if header, err := p.ClientRPC().GetBlockHeaderByHeight(height, p.ctx); err != nil {
			return nil
		} else {
			prevHash, _ := types.HashFromString(header.BlockHeader.PrevHash)
			h, _ := types.HashFromString(header.BlockHeader.Hash)
			blockHeader := &block.Header{
				MajorVersion: uint8(header.BlockHeader.MajorVersion),
				MinorVersion: uint8(header.BlockHeader.MinorVersion),
				Timestamp:    uint64(header.BlockHeader.Timestamp),
				PreviousId:   prevHash,
				Height:       header.BlockHeader.Height,
				Nonce:        uint32(header.BlockHeader.Nonce),
				Reward:       header.BlockHeader.Reward,
				Id:           h,
				Difficulty:   types.DifficultyFrom64(header.BlockHeader.Difficulty),
			}
			return blockHeader
		}
	}

	return nil
}
func (p *P2Pool) GetDifficultyByHeight(height uint64) types.Difficulty {
	lowerThanTip := height <= p.mainchain.GetChainMainTip().Height
	if chainMain := p.mainchain.GetChainMainByHeight(height); chainMain != nil && chainMain.Difficulty != types.ZeroDifficulty {
		return chainMain.Difficulty
	} else if lowerThanTip {
		//TODO cache
		if header, err := p.ClientRPC().GetBlockHeaderByHeight(height, p.ctx); err != nil {
			return types.ZeroDifficulty
		} else {
			prevHash, _ := types.HashFromString(header.BlockHeader.PrevHash)
			h, _ := types.HashFromString(header.BlockHeader.Hash)
			blockHeader := &block.Header{
				MajorVersion: uint8(header.BlockHeader.MajorVersion),
				MinorVersion: uint8(header.BlockHeader.MinorVersion),
				Timestamp:    uint64(header.BlockHeader.Timestamp),
				PreviousId:   prevHash,
				Height:       header.BlockHeader.Height,
				Nonce:        uint32(header.BlockHeader.Nonce),
				Reward:       header.BlockHeader.Reward,
				Id:           h,
				Difficulty:   types.DifficultyFrom64(header.BlockHeader.Difficulty),
			}
			return blockHeader.Difficulty
		}
	}

	return types.ZeroDifficulty
}

// GetMinimalBlockHeaderByHash Only Id / Height / Timestamp are assured
func (p *P2Pool) GetMinimalBlockHeaderByHash(hash types.Hash) *block.Header {
	if chainMain := p.mainchain.GetChainMainByHash(hash); chainMain != nil && chainMain.Id != types.ZeroHash {
		return &block.Header{
			Timestamp:  chainMain.Timestamp,
			Height:     chainMain.Height,
			Reward:     chainMain.Reward,
			Difficulty: chainMain.Difficulty,
			Id:         chainMain.Id,
		}
	} else {
		if header, err := p.ClientRPC().GetBlockHeaderByHash(hash, p.ctx); err != nil || header == nil {
			return nil
		} else {
			prevHash, _ := types.HashFromString(header.PrevHash)
			h, _ := types.HashFromString(header.Hash)
			blockHeader := &block.Header{
				MajorVersion: uint8(header.MajorVersion),
				MinorVersion: uint8(header.MinorVersion),
				Timestamp:    uint64(header.Timestamp),
				PreviousId:   prevHash,
				Height:       header.Height,
				Nonce:        uint32(header.Nonce),
				Reward:       header.Reward,
				Id:           h,
				Difficulty:   types.DifficultyFrom64(header.Difficulty),
			}
			return blockHeader
		}
	}
}

func (p *P2Pool) ClientRPC() *client.Client {
	return p.rpcClient
}

func (p *P2Pool) ClientZMQ() *zmq.Client {
	return p.zmqClient
}

func (p *P2Pool) Context() context.Context {
	return p.ctx
}

func (p *P2Pool) SideChain() *sidechain.SideChain {
	return p.sidechain
}

func (p *P2Pool) MainChain() *mainchain.MainChain {
	return p.mainchain
}

func (p *P2Pool) Server() *p2p.Server {
	return p.server
}

func (p *P2Pool) Run() (err error) {

	if err = p.getInfo(); err != nil {
		return err
	}
	if err = p.getVersion(); err != nil {
		return err
	}
	if err = p.getInfo(); err != nil {
		return err
	}

	if err = p.getMinerData(); err != nil {
		return err
	}

	go func() {
		//TODO: redo listen
		if err := p.mainchain.Listen(); err != nil {
			p.Close(err)
		}
	}()

	//TODO: move peer list loading here

	if p.cache != nil {
		p.cache.LoadAll(p.Server())
	}

	p.started.Store(true)

	if err = p.Server().Listen(); err != nil {
		return err
	}

	return nil
}

func (p *P2Pool) getInfo() error {
	if info, err := p.ClientRPC().GetInfo(); err != nil {
		return err
	} else {
		if info.BusySyncing {
			log.Printf("[P2Pool] monerod is busy syncing, trying again in 5 seconds")
			time.Sleep(time.Second * 5)
			return p.getInfo()
		} else if !info.Synchronized {
			log.Printf("[P2Pool] monerod is not synchronized, trying again in 5 seconds")
			time.Sleep(time.Second * 5)
			return p.getInfo()
		}

		networkType := sidechain.NetworkInvalid
		if info.Mainnet {
			networkType = sidechain.NetworkMainnet
		} else if info.Testnet {
			networkType = sidechain.NetworkTestnet
		} else if info.Stagenet {
			networkType = sidechain.NetworkStagenet
		}

		if p.sidechain.Consensus().NetworkType != networkType {
			return fmt.Errorf("monerod is on %d, but you're mining to a %d sidechain", networkType, p.sidechain.Consensus().NetworkType)
		}

	}
	return nil
}

func (p *P2Pool) getVersion() error {
	if version, err := p.ClientRPC().GetVersion(); err != nil {
		return err
	} else {
		if version.Version < monero.RequiredMoneroVersion {
			return fmt.Errorf("monerod RPC v%d.%d is incompatible, update to RPC >= v%d.%d (Monero %s or newer)", version.Version>>16, version.Version&0xffff, monero.RequiredMajor, monero.RequiredMinor, monero.RequiredMoneroString)
		}
	}
	return nil
}

func (p *P2Pool) getMinerData() error {
	if minerData, err := p.ClientRPC().GetMinerData(); err != nil {
		return err
	} else {
		prevId, _ := types.HashFromString(minerData.PrevId)
		seedHash, _ := types.HashFromString(minerData.SeedHash)
		diff, _ := types.DifficultyFromString(minerData.Difficulty)

		data := &p2pooltypes.MinerData{
			MajorVersion:          minerData.MajorVersion,
			Height:                minerData.Height,
			PrevId:                prevId,
			SeedHash:              seedHash,
			Difficulty:            diff,
			MedianWeight:          minerData.MedianWeight,
			AlreadyGeneratedCoins: minerData.AlreadyGeneratedCoins,
			MedianTimestamp:       minerData.MedianTimestamp,
			TimeReceived:          time.Now(),
		}
		data.TxBacklog = make(mempool.Mempool, len(minerData.TxBacklog))
		for i, e := range minerData.TxBacklog {
			txId, _ := types.HashFromString(e.Id)

			data.TxBacklog[i] = &mempool.MempoolEntry{
				Id:       txId,
				BlobSize: e.BlobSize,
				Weight:   e.Weight,
				Fee:      e.Fee,
			}
		}
		p.mainchain.HandleMinerData(data)

		return p.mainchain.DownloadBlockHeaders(minerData.Height)
	}
}

func (p *P2Pool) Consensus() *sidechain.Consensus {
	return p.consensus
}

func (p *P2Pool) UpdateMainData(data *sidechain.ChainMain) {
	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()
	for i := range p.listeners {
		if p.listeners[i].MainData != nil {
			p.listeners[i].MainData(data)
		}
	}
}

func (p *P2Pool) UpdateMempoolData(data mempool.Mempool) {
	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()
	for i := range p.listeners {
		if p.listeners[i].MempoolData != nil {
			p.listeners[i].MempoolData(data)
		}
	}
}

func (p *P2Pool) UpdateMinerData(data *p2pooltypes.MinerData) {
	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()
	for i := range p.listeners {
		if p.listeners[i].MinerData != nil {
			p.listeners[i].MinerData(data)
		}
	}
}

func (p *P2Pool) UpdateBlockFound(data *sidechain.ChainMain, block *sidechain.PoolBlock) {
	log.Printf("[P2Pool] BLOCK FOUND: main chain block at height %d, id %s was mined by this p2pool", data.Height, data.Id)

	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()
	for i := range p.listeners {
		if p.listeners[i].Found != nil {
			p.listeners[i].Found(data, block)
		}
	}
}

func (p *P2Pool) SubmitBlock(b *block.Block) {

	go func() {
		//do not submit multiple monero blocks to monerod
		if !p.recentSubmittedBlocks.PushUnique(b.Id()) {
			return
		}

		if blob, err := b.MarshalBinary(); err == nil {
			var templateId types.Hash
			var extraNonce uint32
			if t := b.Coinbase.Extra.GetTag(transaction.TxExtraTagMergeMining); t != nil {
				templateId = types.HashFromBytes(t.Data)
			}
			if t := b.Coinbase.Extra.GetTag(transaction.TxExtraTagNonce); t != nil {
				extraNonce = binary.LittleEndian.Uint32(t.Data)
			}
			log.Printf("[P2Pool] submit_block: height = %d, template id = %s, nonce = %d, extra_nonce = %d, blob = %d bytes", b.Coinbase.GenHeight, templateId.String(), b.Nonce, extraNonce, len(blob))

			if result, err := p.ClientRPC().SubmitBlock(blob); err != nil {
				log.Printf("[P2Pool] submit_block: daemon returned error: %s, height = %d, template id = %s, nonce = %d, extra_nonce = %d", err, b.Coinbase.GenHeight, templateId.String(), b.Nonce, extraNonce)
			} else {
				if result.Status == "OK" {
					log.Printf("[P2Pool] submit_block: BLOCK ACCEPTED at height = %d, template id = %s", b.Coinbase.GenHeight, templateId.String())
				} else {
					log.Printf("[P2Pool] submit_block: daemon sent unrecognizable reply: %s, height = %d, template id = %s, nonce = %d, extra_nonce = %d", result.Status, b.Coinbase.GenHeight, templateId.String(), b.Nonce, extraNonce)
				}
			}
		}
	}()
}

func (p *P2Pool) Store(block *sidechain.PoolBlock) {
	if p.cache != nil {
		p.cache.Store(block)
	}
	if p.archive != nil {
		p.archive.Store(block)
	}
}

func (p *P2Pool) ClearCachedBlocks() {
	p.server.ClearCachedBlocks()
}

func (p *P2Pool) Started() bool {
	return p.started.Load()
}

func (p *P2Pool) UpdateTip(tip *sidechain.PoolBlock) {
	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()
	for i := range p.listeners {
		if p.listeners[i].Tip != nil {
			p.listeners[i].Tip(tip)
		}
	}
}

func (p *P2Pool) Broadcast(block *sidechain.PoolBlock) {
	minerData := p.GetMinerDataTip()
	if (block.Main.Coinbase.GenHeight)+2 < minerData.Height {
		log.Printf("[P2Pool] Trying to broadcast a stale block %s (mainchain height %d, current height is %d)", block.SideTemplateId(p.consensus), block.Main.Coinbase.GenHeight, minerData.Height)
		return
	}

	if block.Main.Coinbase.GenHeight > (minerData.Height + 2) {
		log.Printf("[P2Pool] Trying to broadcast a block %s ahead on mainchain (mainchain height %d, current height is %d)", block.SideTemplateId(p.consensus), block.Main.Coinbase.GenHeight, minerData.Height)
		return
	}

	p.server.Broadcast(block)

	p.listenersLock.RLock()
	defer p.listenersLock.RUnlock()
	for i := range p.listeners {
		if p.listeners[i].Broadcast != nil {
			p.listeners[i].Broadcast(block)
		}
	}
}
