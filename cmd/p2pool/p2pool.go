package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	p2pool2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"github.com/gorilla/mux"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

type binaryBlockResult struct {
	Version sidechain.ShareVersion `json:"version"`
	Blob    string                 `json:"blob"`
	Error   string                 `json:"error,omitempty"`
}

type sidechainStatusResult struct {
	Synchronized          bool             `json:"synchronized"`
	Height                uint64           `json:"tip_height"`
	Id                    types.Hash       `json:"tip_id"`
	Difficulty            types.Difficulty `json:"difficulty"`
	CummulativeDifficulty types.Difficulty `json:"cummulative_difficulty"`
	Blocks                int              `json:"blocks"`
}

type serverStatusResult struct {
	PeerId          uint64 `json:"peer_id"`
	SoftwareId      string `json:"software_id"`
	SoftwareVersion string `json:"software_version"`
	ProtocolVersion string `json:"protocol_version"`
	ListenPort      uint16 `json:"listen_port"`
}

type peerResult struct {
	PeerId          uint64 `json:"peer_id"`
	Incoming        bool   `json:"incoming"`
	Address         string `json:"address"`
	SoftwareId      string `json:"software_id"`
	SoftwareVersion string `json:"software_version"`
	ProtocolVersion string `json:"protocol_version"`
	ConnectionTime  uint64 `json:"connection_time"`
	ListenPort      uint32 `json:"listen_port"`
	Latency         uint64 `json:"latency"`
}

func encodeJson(r *http.Request, d any) ([]byte, error) {
	if strings.Index(strings.ToLower(r.Header.Get("user-agent")), "mozilla") != -1 {
		return json.MarshalIndent(d, "", "    ")
	} else {
		return json.Marshal(d)
	}
}

func main() {

	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	currentConsensus := sidechain.ConsensusDefault

	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	moneroZmqPort := flag.Uint("zmq-port", 18083, "monerod ZMQ pub port number")
	p2pListen := flag.String("p2p", fmt.Sprintf("0.0.0.0:%d", currentConsensus.DefaultPort()), "IP:port for p2p server to listen on.")
	createArchive := flag.String("archive", "", "If specified, create an archive store of sidechain blocks on this path.")
	apiBind := flag.String("api-bind", "", "Bind to this address to serve blocks, and other utility methods. If -archive is specified, serve archived blocks.")
	addPeers := flag.String("addpeers", "", "Comma-separated list of IP:port of other p2pool nodes to connect to")
	peerList := flag.String("peer-list", "p2pool_peers.txt", "Either a path or an URL to obtain peer lists from. If it is a path, new peers will be saved to this path")
	consensusConfigFile := flag.String("config", "", "Name of the p2pool config file")
	useMiniSidechain := flag.Bool("mini", false, "Connect to p2pool-mini sidechain. Note that it will also change default p2p port.")

	outPeers := flag.Uint64("out-peers", 10, "Maximum number of outgoing connections for p2p server (any value between 10 and 450)")
	inPeers := flag.Uint64("in-peers", 10, "Maximum number of incoming connections for p2p server (any value between 10 and 450)")
	p2pExternalPort := flag.Uint64("p2p-external-port", 0, "Port number that your router uses for mapping to your local p2p port. Use it if you are behind a NAT and still want to accept incoming connections")

	noCache := flag.Bool("no-cache", false, "Disable p2pool.cache")
	debugLog := flag.Bool("debug", false, "Log more details")
	//TODO extend verbosity to debug flag

	flag.Parse()

	if *debugLog {
		log.SetFlags(log.Flags() | log.Lshortfile)
	}

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	settings := make(map[string]string)
	settings["listen"] = *p2pListen

	changeConsensus := func(newConsensus *sidechain.Consensus) {
		oldListen := netip.MustParseAddrPort(settings["listen"])
		// if default exists, change port to match
		if settings["listen"] == fmt.Sprintf("%s:%d", oldListen.Addr().String(), currentConsensus.DefaultPort()) {
			settings["listen"] = fmt.Sprintf("%s:%d", oldListen.Addr().String(), newConsensus.DefaultPort())
		}
		currentConsensus = newConsensus
	}

	settings["rpc-url"] = fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort)
	settings["zmq-url"] = fmt.Sprintf("tcp://%s:%d", *moneroHost, *moneroZmqPort)
	if *useMiniSidechain {
		changeConsensus(sidechain.ConsensusMini)
	}

	if *consensusConfigFile != "" {
		consensusData, err := os.ReadFile(*consensusConfigFile)
		if err != nil {
			log.Panic(err)
		}

		if newConsensus, err := sidechain.NewConsensusFromJSON(consensusData); err != nil {
			log.Panic(err)
		} else {
			changeConsensus(newConsensus)
		}
	}

	settings["out-peers"] = strconv.FormatUint(*outPeers, 10)
	settings["in-peers"] = strconv.FormatUint(*inPeers, 10)
	settings["external-port"] = strconv.FormatUint(*p2pExternalPort, 10)

	if !*noCache {
		settings["cache"] = "p2pool.cache"
	}

	if *createArchive != "" {
		settings["archive"] = *createArchive
	}

	if instance, err := p2pool2.NewP2Pool(currentConsensus, settings); err != nil {
		log.Fatalf("Could not start p2pool: %s", err)
	} else {
		defer instance.Close()

		if *apiBind != "" {
			serveMux := mux.NewRouter()

			archiveCache := instance.AddressableCache()

			// ================================= Peering section =================================
			serveMux.HandleFunc("/server/peers", func(writer http.ResponseWriter, request *http.Request) {
				clients := instance.Server().Clients()
				result := make([]peerResult, 0, len(clients))
				for _, c := range clients {
					if !c.IsGood() {
						continue
					}
					result = append(result, peerResult{
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
				result := serverStatusResult{
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

			// ================================= SideChain section =================================
			serveMux.HandleFunc("/sidechain/consensus", func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				buf, _ := encodeJson(request, instance.Consensus())
				_, _ = writer.Write(buf)
			})

			serveMux.HandleFunc("/sidechain/blocks_by_height/{height:[0-9]+}", func(writer http.ResponseWriter, request *http.Request) {
				if height, err := strconv.ParseUint(mux.Vars(request)["height"], 10, 0); err == nil {
					result := make([]binaryBlockResult, 0, 1)
					for _, b := range instance.SideChain().GetPoolBlocksByHeight(height) {
						if blob, err := b.MarshalBinary(); err != nil {
							result = append(result, binaryBlockResult{
								Version: b.ShareVersion(),
								Error:   err.Error(),
							})
						} else {
							result = append(result, binaryBlockResult{
								Version: b.ShareVersion(),
								Blob:    hex.EncodeToString(blob),
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
					result := binaryBlockResult{
						Blob:  "",
						Error: "",
					}
					if b := instance.SideChain().GetPoolBlockByTemplateId(templateId); b != nil {
						result.Version = b.ShareVersion()
						if blob, err := b.MarshalBinary(); err != nil {
							result.Error = err.Error()
						} else {
							result.Blob = hex.EncodeToString(blob)
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
			serveMux.HandleFunc("/sidechain/tip", func(writer http.ResponseWriter, request *http.Request) {
				if templateId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
					result := binaryBlockResult{
						Blob:  "",
						Error: "",
					}
					if b := instance.SideChain().GetPoolBlockByTemplateId(templateId); b != nil {
						result.Version = b.ShareVersion()
						if blob, err := b.MarshalBinary(); err != nil {
							result.Error = err.Error()
						} else {
							result.Blob = hex.EncodeToString(blob)
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
			serveMux.HandleFunc("/sidechain/status", func(writer http.ResponseWriter, request *http.Request) {
				result := sidechainStatusResult{
					Synchronized: instance.SideChain().PreCalcFinished(),
					Blocks:       instance.SideChain().GetPoolBlockCount(),
				}
				tip := instance.SideChain().GetChainTip()

				if tip != nil {
					result.Height = tip.Side.Height
					result.Id = tip.SideTemplateId(instance.Consensus())
					result.Difficulty = tip.Side.Difficulty
					result.CummulativeDifficulty = tip.Side.CumulativeDifficulty
				}
				writer.Header().Set("Content-Type", "application/json; charset=utf-8")
				writer.WriteHeader(http.StatusOK)
				buf, _ := encodeJson(request, result)
				_, _ = writer.Write(buf)
			})

			// ================================= Archive section =================================
			if *createArchive != "" && archiveCache != nil {
				serveMux.HandleFunc("/archive/blocks_by_template_id/{id:[0-9a-f]+}", func(writer http.ResponseWriter, request *http.Request) {
					if templateId, err := types.HashFromString(mux.Vars(request)["id"]); err == nil {
						result := make([]binaryBlockResult, 0, 1)
						for _, b := range archiveCache.LoadByTemplateId(templateId) {
							if err := archiveCache.ProcessBlock(b); err != nil {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Error:   err.Error(),
								})
							} else if blob, err := b.MarshalBinary(); err != nil {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Error:   err.Error(),
								})
							} else {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Blob:    hex.EncodeToString(blob),
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
						result := make([]binaryBlockResult, 0, 1)
						for _, b := range archiveCache.LoadBySideChainHeight(height) {
							if err := archiveCache.ProcessBlock(b); err != nil {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Error:   err.Error(),
								})
							} else if blob, err := b.MarshalBinary(); err != nil {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Error:   err.Error(),
								})
							} else {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Blob:    hex.EncodeToString(blob),
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
						result := binaryBlockResult{
							Blob:  "",
							Error: "",
						}
						if b := archiveCache.LoadByMainId(mainId); b != nil {
							result.Version = b.ShareVersion()
							if err := archiveCache.ProcessBlock(b); err != nil {
								result.Error = err.Error()
							} else {
								if blob, err := b.MarshalBinary(); err != nil {
									result.Error = err.Error()
								} else {
									result.Blob = hex.EncodeToString(blob)
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
						result := make([]binaryBlockResult, 0, 1)
						for _, b := range archiveCache.LoadByMainChainHeight(height) {
							if err := archiveCache.ProcessBlock(b); err != nil {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Error:   err.Error(),
								})
							} else if blob, err := b.MarshalBinary(); err != nil {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Error:   err.Error(),
								})
							} else {
								result = append(result, binaryBlockResult{
									Version: b.ShareVersion(),
									Blob:    hex.EncodeToString(blob),
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

			server := &http.Server{
				Addr:        *apiBind,
				ReadTimeout: time.Second * 2,
				Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					if request.Method != "GET" && request.Method != "HEAD" {
						writer.WriteHeader(http.StatusForbidden)
						return
					}

					serveMux.ServeHTTP(writer, request)
				}),
			}

			go func() {
				//TODO: context/wait
				if err := server.ListenAndServe(); err != nil {
					log.Panic(err)
				}

			}()
		}

		var connectList []netip.AddrPort
		for _, peerAddr := range strings.Split(*addPeers, ",") {
			if peerAddr == "" {
				continue
			}

			if addrPort, err := netip.ParseAddrPort(peerAddr); err != nil {
				log.Panic(err)
			} else {
				instance.Server().AddToPeerList(addrPort)
				connectList = append(connectList, addrPort)
			}
		}

		if currentConsensus.SeedNode() != "" {
			log.Printf("Loading seed peers from %s", currentConsensus.SeedNode())
			ips, _ := net.LookupIP(currentConsensus.SeedNode())
			for _, seedNodeIp := range ips {
				seedNodeAddr := netip.MustParseAddrPort(fmt.Sprintf("%s:%d", seedNodeIp.String(), currentConsensus.DefaultPort()))
				instance.Server().AddToPeerList(seedNodeAddr)
				//connectList = append(connectList, seedNodeAddr)
			}
		}

		if *peerList != "" {
			log.Printf("Loading peers from %s", *peerList)
			if len(*peerList) > 4 && (*peerList)[:4] == "http" {
				func() {
					r, err := http.DefaultClient.Get(*peerList)
					if err == nil {
						defer r.Body.Close()
						scanner := bufio.NewScanner(r.Body)
						for scanner.Scan() {
							if addrPort, err := netip.ParseAddrPort(strings.TrimSpace(scanner.Text())); err == nil {
								instance.Server().AddToPeerList(addrPort)
							}
						}
					}
				}()
			} else {
				func() {
					f, err := os.Open(*peerList)
					if err == nil {
						defer f.Close()
						scanner := bufio.NewScanner(f)
						for scanner.Scan() {
							if addrPort, err := netip.ParseAddrPort(strings.TrimSpace(scanner.Text())); err == nil {
								instance.Server().AddToPeerList(addrPort)
							}
						}
					}
				}()

				go func() {
					contents := make([]byte, 0, 4096)
					for range time.Tick(time.Minute * 5) {
						contents = contents[:0]
						for _, addrPort := range instance.Server().PeerList() {
							contents = append(contents, []byte(addrPort.String())...)
							contents = append(contents, '\n')
						}
						if err := os.WriteFile(*peerList, contents, 0644); err != nil {
							log.Printf("error writing %s: %s", *peerList, err.Error())
							break
						}
					}
				}()
			}
		}

		go func() {
			for !instance.Started() {
				time.Sleep(time.Second * 1)
			}

			for _, addrPort := range connectList {
				go func(addrPort netip.AddrPort) {
					if err := instance.Server().Connect(addrPort); err != nil {
						log.Printf("error connecting to peer %s: %s", addrPort.String(), err.Error())
					}
				}(addrPort)
			}
		}()

		if err := instance.Run(); err != nil {
			log.Panic(err)
		}
	}
}
