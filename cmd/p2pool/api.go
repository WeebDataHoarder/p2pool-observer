package main

import (
	"bytes"
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/p2p"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/gorilla/mux"
	"io"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

func getServerMux(instance *p2pool.P2Pool) *mux.Router {

	serveMux := mux.NewRouter()

	archiveCache := instance.AddressableCache()

	// ================================= Peering section =================================
	serveMux.HandleFunc("/server/peers", func(writer http.ResponseWriter, request *http.Request) {
		clients := instance.Server().Clients()
		result := make([]p2pooltypes.P2PoolServerPeerResult, 0, len(clients))
		for _, c := range clients {
			if !c.IsGood() {
				continue
			}
			result = append(result, p2pooltypes.P2PoolServerPeerResult{
				Incoming:        c.IsIncomingConnection,
				Address:         c.AddressPort.Addr().String(),
				SoftwareId:      c.VersionInformation.SoftwareId.String(),
				SoftwareVersion: c.VersionInformation.SoftwareVersion.String(),
				ProtocolVersion: c.VersionInformation.Protocol.String(),
				ConnectionTime:  uint64(c.ConnectionTime.Unix()),
				ListenPort:      c.ListenPort.Load(),
				Latency:         uint64(time.Duration(c.PingDuration.Load()).Milliseconds()),
				PeerId:          c.PeerId.Load(),
			})
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_ = cmdutils.EncodeJson(request, writer, result)
	})

	serveMux.HandleFunc("/server/connection_check/{addrPort:.+}", func(writer http.ResponseWriter, request *http.Request) {
		addrPort, err := netip.ParseAddrPort(mux.Vars(request)["addrPort"])
		if err != nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusBadRequest)
			buf, _ := utils.MarshalJSON(struct {
				Error string `json:"error"`
			}{
				Error: err.Error(),
			})
			_, _ = writer.Write(buf)
			return
		}

		var client *p2p.Client
		var alreadyConnected bool
		isBanned, banEntry := instance.Server().IsBanned(addrPort.Addr())
		for _, c := range instance.Server().Clients() {
			if c.AddressPort.Addr().Compare(addrPort.Addr()) == 0 && uint16(c.ListenPort.Load()) == addrPort.Port() {
				client = c
				alreadyConnected = true
				break
			}
		}
		if client == nil {
			if client, err = instance.Server().DirectConnect(addrPort); err != nil {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusBadRequest)
				buf, _ := utils.MarshalJSON(struct {
					Error string `json:"error"`
				}{
					Error: err.Error(),
				})
				_, _ = writer.Write(buf)
				return
			}
		}

		if client == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusBadRequest)
			buf, _ := utils.MarshalJSON(struct {
				Error string `json:"error"`
			}{
				Error: "could not find client",
			})
			_, _ = writer.Write(buf)
			return
		}

		for i := 0; i < 6; i++ {
			if client.Closed.Load() || (client.IsGood() && client.PingDuration.Load() > 0 && client.LastKnownTip.Load() != nil) {
				break
			}
			time.Sleep(time.Second * 1)
		}

		banError := ""
		errorStr := ""
		if err := client.BanError(); err != nil {
			errorStr = err.Error()
		}

		if banEntry != nil && banEntry.Error != nil {
			banError = banEntry.Error.Error()
		}

		info := p2pooltypes.P2PoolConnectionCheckInformation{
			Address:           client.AddressPort.Addr().Unmap().String(),
			Port:              client.AddressPort.Port(),
			ListenPort:        uint16(client.ListenPort.Load()),
			PeerId:            client.PeerId.Load(),
			SoftwareId:        client.VersionInformation.SoftwareId.String(),
			SoftwareVersion:   client.VersionInformation.SoftwareVersion.String(),
			ProtocolVersion:   client.VersionInformation.Protocol.String(),
			ConnectionTime:    uint64(client.ConnectionTime.Unix()),
			Latency:           uint64(time.Duration(client.PingDuration.Load()).Milliseconds()),
			Incoming:          client.IsIncomingConnection,
			BroadcastTime:     client.LastBroadcastTimestamp.Load(),
			BroadcastHeight:   client.BroadcastMaxHeight.Load(),
			Tip:               client.LastKnownTip.Load(),
			Closed:            client.Closed.Load(),
			AlreadyConnected:  alreadyConnected,
			HandshakeComplete: client.HandshakeComplete.Load(),
			LastActive:        client.LastActiveTimestamp.Load(),
			Banned:            isBanned,
			Error:             errorStr,
			BanError:          banError,
		}

		if isBanned {
			client.Close()
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_ = cmdutils.EncodeJson(request, writer, info)
	})

	serveMux.HandleFunc("/server/peerlist", func(writer http.ResponseWriter, request *http.Request) {
		peers := instance.Server().PeerList()
		var result []byte
		for _, c := range peers {
			result = append(result, []byte(c.AddressPort.String()+"\n")...)
		}
		writer.Header().Set("Content-Type", "text/plain")
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write(result)
	})

	serveMux.HandleFunc("/server/status", func(writer http.ResponseWriter, request *http.Request) {
		result := p2pooltypes.P2PoolServerStatusResult{
			PeerId:          instance.Server().PeerId(),
			SoftwareId:      instance.Server().VersionInformation().SoftwareId.String(),
			SoftwareVersion: instance.Server().VersionInformation().SoftwareVersion.String(),
			ProtocolVersion: instance.Server().VersionInformation().Protocol.String(),
			ListenPort:      instance.Server().ListenPort(),
		}

		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_ = cmdutils.EncodeJson(request, writer, result)
	})

	// ================================= MainChain section =================================
	serveMux.HandleFunc("/mainchain/header_by_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
		if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
			result := instance.GetMinimalBlockHeaderByHeight(height)
			if result == nil {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusNotFound)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_ = cmdutils.EncodeJson(request, writer, result)
			}
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}
	})
	serveMux.HandleFunc("/mainchain/difficulty_by_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
		if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
			result := instance.GetDifficultyByHeight(height)
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_ = cmdutils.EncodeJson(request, writer, result)
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}
	})
	serveMux.HandleFunc("/mainchain/header_by_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
		if id, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
			result := instance.GetMinimalBlockHeaderByHash(id)
			if result == nil {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusNotFound)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_ = cmdutils.EncodeJson(request, writer, result)
			}
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}
	})
	serveMux.HandleFunc("/mainchain/miner_data", func(writer http.ResponseWriter, request *http.Request) {
		result := instance.GetMinerDataTip()
		if result == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_ = cmdutils.EncodeJson(request, writer, result)
		}
	})
	serveMux.HandleFunc("/mainchain/tip", func(writer http.ResponseWriter, request *http.Request) {
		minerData := instance.GetMinerDataTip()
		if minerData == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		result := instance.GetMinimalBlockHeaderByHeight(minerData.Height - 1)
		if result == nil {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_ = cmdutils.EncodeJson(request, writer, result)
		}
	})

	// ================================= SideChain section =================================
	serveMux.HandleFunc("/sidechain/consensus", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_ = cmdutils.EncodeJson(request, writer, instance.Consensus())
	})

	serveMux.HandleFunc("/sidechain/blocks_by_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
		if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
			result := make([]p2pooltypes.P2PoolBinaryBlockResult, 0, 1)
			for _, b := range instance.SideChain().GetPoolBlocksByHeight(height) {
				if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
					result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Error:   err.Error(),
					})
				} else {
					result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Blob:    blob,
					})
				}
			}
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_ = cmdutils.EncodeJson(request, writer, result)
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("[]"))
		}
	})
	serveMux.HandleFunc("/sidechain/block_by_template_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
		if templateId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
			var result p2pooltypes.P2PoolBinaryBlockResult
			if b := instance.SideChain().GetPoolBlockByTemplateId(templateId); b != nil {
				result.Version = int(b.ShareVersion())
				if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
					result.Error = err.Error()
				} else {
					result.Blob = blob
				}
			}
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_ = cmdutils.EncodeJson(request, writer, result)
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("[]"))
		}
	})
	serveMux.HandleFunc("/sidechain/state/short", func(writer http.ResponseWriter, request *http.Request) {
		tip := instance.SideChain().GetChainTip()
		if tip != nil {
			tipId := instance.SideChain().GetChainTip().SideTemplateId(instance.Consensus())
			chain, uncles := instance.SideChain().GetPoolBlocksFromTip(tipId)
			result := p2pooltypes.P2PoolSideChainStateResult{
				TipHeight: tip.Side.Height,
				TipId:     tipId,
				Chain:     make([]p2pooltypes.P2PoolBinaryBlockResult, 0, len(chain)),
				Uncles:    make([]p2pooltypes.P2PoolBinaryBlockResult, 0, len(uncles)),
			}
			for _, b := range chain {
				if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
					result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Error:   err.Error(),
					})
				} else {
					result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Blob:    blob,
					})
				}
			}
			for _, b := range uncles {
				if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
					result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Error:   err.Error(),
					})
				} else {
					result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Blob:    blob,
					})
				}
			}
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_ = cmdutils.EncodeJson(request, writer, result)
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}
	})
	serveMux.HandleFunc("/sidechain/state/tip", func(writer http.ResponseWriter, request *http.Request) {
		tip := instance.SideChain().GetChainTip()
		if tip != nil {
			tipId := instance.SideChain().GetChainTip().SideTemplateId(instance.Consensus())
			chain, uncles := instance.SideChain().GetPoolBlocksFromTip(tipId)
			result := p2pooltypes.P2PoolSideChainStateResult{
				TipHeight: tip.Side.Height,
				TipId:     tipId,
				Chain:     make([]p2pooltypes.P2PoolBinaryBlockResult, 0, len(chain)),
				Uncles:    make([]p2pooltypes.P2PoolBinaryBlockResult, 0, len(uncles)),
			}
			for _, b := range chain {
				if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
					result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Error:   err.Error(),
					})
				} else {
					result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Blob:    blob,
					})
				}
			}
			for _, b := range uncles {
				if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
					result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Error:   err.Error(),
					})
				} else {
					result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
						Version: int(b.ShareVersion()),
						Blob:    blob,
					})
				}
			}
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_ = cmdutils.EncodeJson(request, writer, result)
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}
	})
	serveMux.HandleFunc("/sidechain/state/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
		if templateId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
			tip := instance.SideChain().GetPoolBlockByTemplateId(templateId)
			if tip != nil {
				tipId := tip.SideTemplateId(instance.Consensus())
				chain, uncles := instance.SideChain().GetPoolBlocksFromTip(tipId)
				result := p2pooltypes.P2PoolSideChainStateResult{
					TipHeight: tip.Side.Height,
					TipId:     tipId,
					Chain:     make([]p2pooltypes.P2PoolBinaryBlockResult, 0, len(chain)),
					Uncles:    make([]p2pooltypes.P2PoolBinaryBlockResult, 0, len(uncles)),
				}
				for _, b := range chain {
					if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
						result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else {
						result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Blob:    blob,
						})
					}
				}
				for _, b := range uncles {
					if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
						result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else {
						result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Blob:    blob,
						})
					}
				}
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_ = cmdutils.EncodeJson(request, writer, result)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("{}"))
			}
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}
	})
	serveMux.HandleFunc("/sidechain/tip", func(writer http.ResponseWriter, request *http.Request) {
		var result p2pooltypes.P2PoolBinaryBlockResult
		if b := instance.SideChain().GetChainTip(); b != nil {
			result.Version = int(b.ShareVersion())
			if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
				result.Error = err.Error()
			} else {
				result.Blob = blob
			}
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_ = cmdutils.EncodeJson(request, writer, result)
	})
	serveMux.HandleFunc("/sidechain/status", func(writer http.ResponseWriter, request *http.Request) {
		result := p2pooltypes.P2PoolSideChainStatusResult{
			Synchronized: instance.SideChain().PreCalcFinished(),
			Blocks:       instance.SideChain().GetPoolBlockCount(),
		}
		tip := instance.SideChain().GetHighestKnownTip()

		if tip != nil {
			result.Height = tip.Side.Height
			result.Id = tip.SideTemplateId(instance.Consensus())
			result.Difficulty = tip.Side.Difficulty
			result.CumulativeDifficulty = tip.Side.CumulativeDifficulty
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		_ = cmdutils.EncodeJson(request, writer, result)
	})

	preAllocatedSharesPool := sidechain.NewPreAllocatedSharesPool(instance.Consensus().ChainWindowSize * 2)

	// ================================= Archive section =================================
	if archiveCache != nil {
		serveMux.HandleFunc("/archive/window_from_template_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
			if templateId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
				tpls := archiveCache.LoadByTemplateId(templateId)
				if len(tpls) > 0 {
					tip := tpls[0]

					mainDifficulty := instance.GetDifficultyByHeight(randomx.SeedHeight(tip.Main.Coinbase.GenHeight))

					expectedBlocks := int(mainDifficulty.Mul64(2).Div(tip.Side.Difficulty).Lo) + 20
					if expectedBlocks < 100 {
						expectedBlocks = 100
					}
					if expectedBlocks > int(instance.Consensus().ChainWindowSize) {
						expectedBlocks = int(instance.Consensus().ChainWindowSize)
					}
					if tip.ShareVersion() == sidechain.ShareVersion_V1 {
						expectedBlocks = int(instance.Consensus().ChainWindowSize)
					}

					var windowLock sync.RWMutex
					window := make(sidechain.UniquePoolBlockSlice, 0, expectedBlocks)
					window = append(window, tip)

					getByTemplateId := func(id types.Hash) *sidechain.PoolBlock {
						if b := func() *sidechain.PoolBlock {
							windowLock.RLock()
							defer windowLock.RUnlock()
							if b := window.Get(id); b != nil {
								return b
							}
							return nil
						}(); b != nil {
							return b
						}
						windowLock.Lock()
						defer windowLock.Unlock()
						if bs := archiveCache.LoadByTemplateId(id); len(bs) > 0 {
							window = append(window, bs[0])
							return bs[0]
						}
						return nil
					}

					preAllocatedShares := preAllocatedSharesPool.Get()
					defer preAllocatedSharesPool.Put(preAllocatedShares)

					if _, err = tip.PreProcessBlock(instance.Consensus(), instance.SideChain().DerivationCache(), preAllocatedShares, instance.GetDifficultyByHeight, getByTemplateId); err == nil {
						result := p2pooltypes.P2PoolSideChainStateResult{
							TipHeight: tip.Side.Height,
							TipId:     tip.SideTemplateId(instance.Consensus()),
							Chain:     make([]p2pooltypes.P2PoolBinaryBlockResult, 0, expectedBlocks),
							Uncles:    make([]p2pooltypes.P2PoolBinaryBlockResult, 0, expectedBlocks/5),
						}

						var topError error

						for e := range sidechain.IterateBlocksInPPLNSWindow(tip, instance.Consensus(), instance.GetDifficultyByHeight, getByTemplateId, nil, func(err error) {
							topError = err
						}) {
							if _, err = e.Block.PreProcessBlock(instance.Consensus(), instance.SideChain().DerivationCache(), preAllocatedShares, instance.GetDifficultyByHeight, getByTemplateId); err != nil {
								topError = err
								result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
									Version: int(e.Block.ShareVersion()),
									Error:   err.Error(),
								})
							} else if blob, err := e.Block.AppendBinaryFlags(make([]byte, 0, e.Block.BufferLength()), false, false); err != nil {
								topError = err
								result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
									Version: int(e.Block.ShareVersion()),
									Error:   err.Error(),
								})
							} else {
								result.Chain = append(result.Chain, p2pooltypes.P2PoolBinaryBlockResult{
									Version: int(e.Block.ShareVersion()),
									Blob:    blob,
								})
							}

							for _, u := range e.Uncles {
								if _, err = u.PreProcessBlock(instance.Consensus(), instance.SideChain().DerivationCache(), preAllocatedShares, instance.GetDifficultyByHeight, getByTemplateId); err != nil {
									topError = err
									result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
										Version: int(u.ShareVersion()),
										Error:   err.Error(),
									})
								} else if blob, err := u.AppendBinaryFlags(make([]byte, 0, u.BufferLength()), false, false); err != nil {
									topError = err
									result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
										Version: int(u.ShareVersion()),
										Error:   err.Error(),
									})
								} else {
									result.Uncles = append(result.Uncles, p2pooltypes.P2PoolBinaryBlockResult{
										Version: int(u.ShareVersion()),
										Blob:    blob,
									})
								}
							}
						}

						if topError == nil {
							writer.Header().Set("Content-Type", "application/json; charset=utf-8")
							writer.WriteHeader(http.StatusOK)
							_ = cmdutils.EncodeJson(request, writer, result)
							return
						}
					}
				}
			}

			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write([]byte("{}"))
		})
		serveMux.HandleFunc("/archive/store_alternate", func(writer http.ResponseWriter, request *http.Request) {
			if buf, err := io.ReadAll(request.Body); err != nil {
				return
			} else {
				b := &sidechain.PoolBlock{}

				if err = b.UnmarshalBinary(instance.Consensus(), instance.SideChain().DerivationCache(), buf); err != nil {
					return
				}
				if b.NeedsPreProcess() {
					return
				}
				templateId := b.SideTemplateId(instance.Consensus())
				if bytes.Compare(b.CoinbaseExtra(sidechain.SideTemplateId), templateId[:]) != 0 {
					return
				}
				if archiveCache.LoadByMainId(b.MainId()) != nil {
					return
				}

				existingBlock := archiveCache.LoadByTemplateId(templateId)

				if len(existingBlock) == 0 {
					return
				}
				tempData, _ := existingBlock[0].MarshalBinary()

				newBlock := &sidechain.PoolBlock{}
				if err = newBlock.UnmarshalBinary(instance.Consensus(), instance.SideChain().DerivationCache(), tempData); err != nil {
					return
				}
				//set extra nonce and nonce
				newBlock.Main.Coinbase.Extra[1] = b.Main.Coinbase.Extra[1]
				newBlock.Main.Nonce = b.Main.Nonce
				newBlock.Depth.Store(math.MaxUint64)

				if !newBlock.IsProofHigherThanDifficulty(instance.Consensus().GetHasher(), func(height uint64) (hash types.Hash) {
					seedHeight := randomx.SeedHeight(height)
					if h := instance.GetMinimalBlockHeaderByHeight(seedHeight); h != nil {
						return h.Id
					} else {
						return types.ZeroHash
					}
				}) {
					return
				}

				//finally store alternate
				archiveCache.Store(newBlock)

			}
		})
		serveMux.HandleFunc("/archive/blocks_by_template_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
			if templateId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
				result := make([]p2pooltypes.P2PoolBinaryBlockResult, 0, 1)
				for _, b := range archiveCache.LoadByTemplateId(templateId) {
					if err := archiveCache.ProcessBlock(b); err != nil {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Blob:    blob,
						})
					}
				}
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_ = cmdutils.EncodeJson(request, writer, result)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("[]"))
			}
		})
		serveMux.HandleFunc("/archive/blocks_by_side_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
			if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
				result := make([]p2pooltypes.P2PoolBinaryBlockResult, 0, 1)
				for _, b := range archiveCache.LoadBySideChainHeight(height) {
					if err := archiveCache.ProcessBlock(b); err != nil {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Blob:    blob,
						})
					}
				}
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_ = cmdutils.EncodeJson(request, writer, result)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("[]"))
			}
		})

		serveMux.HandleFunc("/archive/light_blocks_by_main_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
			if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
				if blocks := archiveCache.LoadByMainChainHeight(height); len(blocks) > 0 {
					writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					writer.WriteHeader(http.StatusOK)
					_ = cmdutils.EncodeJson(request, writer, blocks)
				} else {
					writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					writer.WriteHeader(http.StatusNotFound)
					if blocks == nil {
						blocks = make(sidechain.UniquePoolBlockSlice, 0)
					}
					_ = cmdutils.EncodeJson(request, writer, blocks)
				}
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte("{}"))
			}
		})

		serveMux.HandleFunc("/archive/light_blocks_by_side_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
			if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
				if blocks := archiveCache.LoadBySideChainHeight(height); len(blocks) > 0 {
					writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					writer.WriteHeader(http.StatusOK)
					_ = cmdutils.EncodeJson(request, writer, blocks)
				} else {
					writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					writer.WriteHeader(http.StatusNotFound)
					if blocks == nil {
						blocks = make(sidechain.UniquePoolBlockSlice, 0)
					}
					_ = cmdutils.EncodeJson(request, writer, blocks)
				}
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte("{}"))
			}
		})

		serveMux.HandleFunc("/archive/light_blocks_by_template_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
			if mainId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
				if blocks := archiveCache.LoadByTemplateId(mainId); len(blocks) > 0 {
					writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					writer.WriteHeader(http.StatusOK)
					_ = cmdutils.EncodeJson(request, writer, blocks)
				} else {
					writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					writer.WriteHeader(http.StatusNotFound)
					if blocks == nil {
						blocks = make(sidechain.UniquePoolBlockSlice, 0)
					}
					_ = cmdutils.EncodeJson(request, writer, blocks)
				}
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte("{}"))
			}
		})

		serveMux.HandleFunc("/archive/light_block_by_main_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
			if mainId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
				if b := archiveCache.LoadByMainId(mainId); b != nil {
					writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					writer.WriteHeader(http.StatusOK)
					_ = cmdutils.EncodeJson(request, writer, b)
				} else {
					writer.Header().Set("Content-Type", "application/json; charset=utf-8")
					writer.WriteHeader(http.StatusNotFound)
					_, _ = writer.Write([]byte("{}"))
				}
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte("{}"))
			}
		})

		serveMux.HandleFunc("/archive/block_by_main_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
			if mainId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
				var result p2pooltypes.P2PoolBinaryBlockResult
				if b := archiveCache.LoadByMainId(mainId); b != nil {
					result.Version = int(b.ShareVersion())
					if err := archiveCache.ProcessBlock(b); err != nil {
						result.Error = err.Error()
					} else {
						if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
							result.Error = err.Error()
						} else {
							result.Blob = blob
						}
					}
				}
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_ = cmdutils.EncodeJson(request, writer, result)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(""))
			}
		})
		serveMux.HandleFunc("/archive/blocks_by_main_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
			if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
				result := make([]p2pooltypes.P2PoolBinaryBlockResult, 0, 1)
				for _, b := range archiveCache.LoadByMainChainHeight(height) {
					if err := archiveCache.ProcessBlock(b); err != nil {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else if blob, err := b.AppendBinaryFlags(make([]byte, 0, b.BufferLength()), false, false); err != nil {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Blob:    blob,
						})
					}
				}
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_ = cmdutils.EncodeJson(request, writer, result)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("[]"))
			}
		})
	}

	return serveMux
}
