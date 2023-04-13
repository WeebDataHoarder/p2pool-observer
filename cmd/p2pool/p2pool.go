package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/randomx"
	p2poolinstance "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

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
	lightMode := flag.Bool("light-mode", false, "Don't allocate RandomX dataset, saves 2GB of RAM")
	peerList := flag.String("peer-list", "p2pool_peers.txt", "Either a path or an URL to obtain peer lists from. If it is a path, new peers will be saved to this path")
	consensusConfigFile := flag.String("consensus-config", "", "Name of the p2pool consensus config file")
	useMiniSidechain := flag.Bool("mini", false, "Connect to p2pool-mini sidechain. Note that it will also change default p2p port.")

	outPeers := flag.Uint64("out-peers", 10, "Maximum number of outgoing connections for p2p server (any value between 10 and 450)")
	inPeers := flag.Uint64("in-peers", 10, "Maximum number of incoming connections for p2p server (any value between 10 and 450)")
	p2pExternalPort := flag.Uint64("p2p-external-port", 0, "Port number that your router uses for mapping to your local p2p port. Use it if you are behind a NAT and still want to accept incoming connections")

	memoryLimitInGiB := flag.Uint64("memory-limit", 0, "Memory limit for go managed sections in GiB, set 0 to disable")

	noCache := flag.Bool("no-cache", false, "Disable p2pool.cache")
	debugLog := flag.Bool("debug", false, "Log more details")
	//TODO extend verbosity to debug flag
	flag.Parse()

	if *debugLog {
		log.SetFlags(log.Flags() | log.Lshortfile)
	}

	randomx.UseFullMemory.Store(!*lightMode)

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	debug.SetTraceback("all")
	if *memoryLimitInGiB != 0 {
		debug.SetMemoryLimit(int64(*memoryLimitInGiB) * 1024 * 1024 * 1024)
	}

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

	if instance, err := p2poolinstance.NewP2Pool(currentConsensus, settings); err != nil {
		log.Fatalf("Could not start p2pool: %s", err)
	} else {
		defer instance.Close(nil)

		if *apiBind != "" {

			serveMux := getServerMux(instance)

			server := &http.Server{
				Addr:        *apiBind,
				ReadTimeout: time.Second * 2,
				Handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					if request.Method != "GET" && request.Method != "HEAD" {
						writer.WriteHeader(http.StatusForbidden)
						return
					}

					log.Printf("handling %s", request.URL.String())

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
