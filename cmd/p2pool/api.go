package main

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	p2pooltypes "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/gorilla/mux"
	"net/http"
	"strconv"
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
		buf, _ := encodeJson(request, result)
		_, _ = writer.Write(buf)
	})

	serveMux.HandleFunc("/server/peerlist", func(writer http.ResponseWriter, request *http.Request) {
		peers := instance.Server().PeerList()
		var result []byte
		for _, c := range peers {
			result = append(result, []byte(c.String()+"\n")...)
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
		buf, _ := encodeJson(request, result)
		_, _ = writer.Write(buf)
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
				buf, _ := encodeJson(request, result)
				_, _ = writer.Write(buf)
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
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}
	})
	serveMux.HandleFunc("/sidechain/header_by_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
		if id, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
			result := instance.GetMinimalBlockHeaderByHash(id)
			if result == nil {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusNotFound)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				buf, _ := encodeJson(request, result)
				_, _ = writer.Write(buf)
			}
		} else {
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}
	})

	// ================================= SideChain section =================================
	serveMux.HandleFunc("/sidechain/consensus", func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, instance.Consensus())
		_, _ = writer.Write(buf)
	})

	serveMux.HandleFunc("/sidechain/blocks_by_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
		if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
			result := make([]p2pooltypes.P2PoolBinaryBlockResult, 0, 1)
			for _, b := range instance.SideChain().GetPoolBlocksByHeight(height) {
				if blob, err := b.MarshalBinary(); err != nil {
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
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)
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
				if blob, err := b.MarshalBinary(); err != nil {
					result.Error = err.Error()
				} else {
					result.Blob = blob
				}
			}
			writer.Header().Set("Content-Type", "application/json; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)
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
				if blob, err := b.MarshalBinary(); err != nil {
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
				if blob, err := b.MarshalBinary(); err != nil {
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
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)
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
				if blob, err := b.MarshalBinary(); err != nil {
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
				if blob, err := b.MarshalBinary(); err != nil {
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
			buf, _ := encodeJson(request, result)
			_, _ = writer.Write(buf)
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
					if blob, err := b.MarshalBinary(); err != nil {
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
					if blob, err := b.MarshalBinary(); err != nil {
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
				buf, _ := encodeJson(request, result)
				_, _ = writer.Write(buf)
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
			if blob, err := b.MarshalBinary(); err != nil {
				result.Error = err.Error()
			} else {
				result.Blob = blob
			}
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, result)
		_, _ = writer.Write(buf)
	})
	serveMux.HandleFunc("/sidechain/status", func(writer http.ResponseWriter, request *http.Request) {
		result := p2pooltypes.P2PoolSideChainStatusResult{
			Synchronized: instance.SideChain().PreCalcFinished(),
			Blocks:       instance.SideChain().GetPoolBlockCount(),
		}
		tip := instance.SideChain().GetChainTip()

		if tip != nil {
			result.Height = tip.Side.Height
			result.Id = tip.SideTemplateId(instance.Consensus())
			result.Difficulty = tip.Side.Difficulty
			result.CumulativeDifficulty = tip.Side.CumulativeDifficulty
		}
		writer.Header().Set("Content-Type", "application/json; charset=utf-8")
		writer.WriteHeader(http.StatusOK)
		buf, _ := encodeJson(request, result)
		_, _ = writer.Write(buf)
	})

	// ================================= Archive section =================================
	if archiveCache != nil {
		serveMux.HandleFunc("/archive/blocks_by_template_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
			if templateId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
				result := make([]p2pooltypes.P2PoolBinaryBlockResult, 0, 1)
				for _, b := range archiveCache.LoadByTemplateId(templateId) {
					if err := archiveCache.ProcessBlock(b); err != nil {
						result = append(result, p2pooltypes.P2PoolBinaryBlockResult{
							Version: int(b.ShareVersion()),
							Error:   err.Error(),
						})
					} else if blob, err := b.MarshalBinary(); err != nil {
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
				buf, _ := encodeJson(request, result)
				_, _ = writer.Write(buf)
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
					} else if blob, err := b.MarshalBinary(); err != nil {
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
				buf, _ := encodeJson(request, result)
				_, _ = writer.Write(buf)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("[]"))
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
						if blob, err := b.MarshalBinary(); err != nil {
							result.Error = err.Error()
						} else {
							result.Blob = blob
						}
					}
				}
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				buf, _ := encodeJson(request, result)
				_, _ = writer.Write(buf)
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
					} else if blob, err := b.MarshalBinary(); err != nil {
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
				buf, _ := encodeJson(request, result)
				_, _ = writer.Write(buf)
			} else {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte("[]"))
			}
		})
	}

	return serveMux
}
