package p2pool

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/block"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client/zmq"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/transaction"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/mainchain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/p2p"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"log"
	"strconv"
	"sync/atomic"
	"time"
)

type P2Pool struct {
	consensus *sidechain.Consensus
	sidechain *sidechain.SideChain
	mainchain *mainchain.MainChain
	server    *p2p.Server

	ctx       context.Context
	ctxCancel context.CancelFunc

	rpcClient *client.Client
	zmqClient *zmq.Client

	started atomic.Bool
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

func (p *P2Pool) Close() {
	p.ctxCancel()
	_ = p.zmqClient.Close()
}

func NewP2Pool(consensus *sidechain.Consensus, settings map[string]string) (*P2Pool, error) {
	pool := &P2Pool{
		consensus: consensus,
	}
	var err error

	pool.ctx, pool.ctxCancel = context.WithCancel(context.Background())

	listenAddress := fmt.Sprintf("0.0.0.0:%d", pool.Consensus().DefaultPort())

	if pool.rpcClient, err = client.NewClient(settings["rpc-url"]); err != nil {
		return nil, err
	}

	pool.zmqClient = zmq.NewClient(settings["zmq-url"], zmq.TopicFullChainMain, zmq.TopicFullMinerData)

	pool.sidechain = sidechain.NewSideChain(pool)

	pool.mainchain = mainchain.NewMainChain(pool.sidechain, pool)

	if pool.mainchain == nil {
		return nil, errors.New("could not create MainChain")
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

	if pool.server, err = p2p.NewServer(pool, listenAddress, uint16(externalListenPort), uint32(maxOutgoingPeers), uint32(maxIncomingPeers), pool.ctx); err != nil {
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
		return &block.Header{
			Timestamp:  chainMain.Timestamp,
			Height:     chainMain.Height,
			Reward:     chainMain.Reward,
			Difficulty: chainMain.Difficulty,
			Id:         chainMain.Id,
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
			//cache it. next block found will clean it up
			p.mainchain.HandleMainHeader(blockHeader)
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
			//cache it. next block found will clean it up
			p.mainchain.HandleMainHeader(blockHeader)
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
		if header, err := p.ClientRPC().GetBlockHeaderByHash(hash, p.ctx); err != nil {
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
			//cache it. next block found will clean it up
			p.mainchain.HandleMainHeader(blockHeader)
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
		if p.mainchain.Listen() != nil {
			p.Close()
		}
	}()

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

const RequiredMajor = 3
const RequiredMinor = 10

const RequiredMoneroVersion = (RequiredMajor << 16) | RequiredMinor

func (p *P2Pool) getVersion() error {
	if version, err := p.ClientRPC().GetVersion(); err != nil {
		return err
	} else {
		if version.Version < RequiredMoneroVersion {
			return fmt.Errorf("monerod RPC v%d.%d is incompatible, update to RPC >= v%d.%d (Monero v0.18.0.0 or newer)", version.Version>>16, version.Version&0xffff, RequiredMajor, RequiredMinor)
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
		p.mainchain.HandleMinerData(&p2pooltypes.MinerData{
			MajorVersion:          minerData.MajorVersion,
			Height:                minerData.Height,
			PrevId:                prevId,
			SeedHash:              seedHash,
			Difficulty:            diff,
			MedianWeight:          minerData.MedianWeight,
			AlreadyGeneratedCoins: minerData.AlreadyGeneratedCoins,
			MedianTimestamp:       minerData.MedianTimestamp,
			TimeReceived:          time.Now(),
		})

		return p.mainchain.DownloadBlockHeaders(minerData.Height)
	}
}

func (p *P2Pool) Consensus() *sidechain.Consensus {
	return p.consensus
}

func (p *P2Pool) UpdateBlockFound(data *sidechain.ChainMain, block *sidechain.PoolBlock) {
	log.Printf("[P2Pool] BLOCK FOUND: main chain block at height %d, id %s was mined by this p2pool", data.Height, data.Id)
	//TODO
}

func (p *P2Pool) SubmitBlock(b *block.Block) {

	//TODO: do not submit multiple times
	go func() {
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

func (p *P2Pool) Started() bool {
	return p.started.Load()
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
}
