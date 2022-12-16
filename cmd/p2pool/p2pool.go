package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	p2pool2 "git.gammaspectra.live/P2Pool/p2pool-observer/p2pool"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	currentConsensus := sidechain.ConsensusDefault

	moneroHost := flag.String("host", "127.0.0.1", "IP address of your Monero node")
	moneroRpcPort := flag.Uint("rpc-port", 18081, "monerod RPC API port number")
	p2pListen := flag.String("p2p", fmt.Sprintf("0.0.0.0:%d", currentConsensus.DefaultPort()), "IP:port for p2p server to listen on.")
	//TODO: zmq
	addPeers := flag.String("addpeers", "", "Comma-separated list of IP:port of other p2pool nodes to connect to")
	peerList := flag.String("peer-list", "p2pool_peers.txt", "Either a path or an URL to obtain peer lists from. If it is a path, new peers will be saved to this path")
	consensusConfigFile := flag.String("config", "", "Name of the p2pool config file")
	useMiniSidechain := flag.Bool("mini", false, "Connect to p2pool-mini sidechain. Note that it will also change default p2p port.")

	outPeers := flag.Uint64("out-peers", 10, "Maximum number of outgoing connections for p2p server (any value between 10 and 450)")
	inPeers := flag.Uint64("in-peers", 10, "Maximum number of incoming connections for p2p server (any value between 10 and 450)")
	p2pExternalPort := flag.Uint64("p2p-external-port", 0, "Port number that your router uses for mapping to your local p2p port. Use it if you are behind a NAT and still want to accept incoming connections")

	flag.Parse()

	client.SetDefaultClientSettings(fmt.Sprintf("http://%s:%d", *moneroHost, *moneroRpcPort))

	settings := make(map[string]string)
	settings["listen"] = *p2pListen
	if *useMiniSidechain {
		if settings["listen"] == fmt.Sprintf("0.0.0.0:%d", currentConsensus.DefaultPort()) {
			settings["listen"] = fmt.Sprintf("0.0.0.0:%d", sidechain.ConsensusMini.DefaultPort())
		}
		currentConsensus = sidechain.ConsensusMini
	}

	if *consensusConfigFile != "" {
		consensusData, err := os.ReadFile(*consensusConfigFile)
		if err != nil {
			log.Panic(err)
		}
		var newConsensus sidechain.Consensus
		if err = json.Unmarshal(consensusData, &newConsensus); err != nil {
			log.Panic(err)
		}

		if settings["listen"] == fmt.Sprintf("0.0.0.0:%d", currentConsensus.DefaultPort()) {
			settings["listen"] = fmt.Sprintf("0.0.0.0:%d", newConsensus.DefaultPort())
		}
		currentConsensus = &newConsensus
	}

	settings["out-peers"] = strconv.FormatUint(*outPeers, 10)
	settings["in-peers"] = strconv.FormatUint(*inPeers, 10)
	settings["external-port"] = strconv.FormatUint(*p2pExternalPort, 10)

	if p2pool := p2pool2.NewP2Pool(currentConsensus, settings); p2pool == nil {
		log.Fatal("Could not start p2pool")
	} else {
		for _, peerAddr := range strings.Split(*addPeers, ",") {
			if peerAddr == "" {
				continue
			}

			if addrPort, err := netip.ParseAddrPort(peerAddr); err != nil {
				log.Panic(err)
			} else {
				p2pool.Server().AddToPeerList(addrPort)
				go func() {
					if err := p2pool.Server().Connect(addrPort); err != nil {
						log.Printf("error connecting to peer %s: %s", addrPort.String(), err.Error())
					}
				}()
			}
		}

		if currentConsensus.SeedNode() != "" {
			log.Printf("Loading seed peers from %s", currentConsensus.SeedNode())
			ips, _ := net.LookupIP(currentConsensus.SeedNode())
			for _, seedNodeIp := range ips {
				seedNodeAddr := netip.MustParseAddrPort(fmt.Sprintf("%s:%d", seedNodeIp.String(), currentConsensus.DefaultPort()))
				p2pool.Server().AddToPeerList(seedNodeAddr)
				go func() {
					if err := p2pool.Server().Connect(seedNodeAddr); err != nil {
						log.Printf("error connecting to seed node peer %s: %s", seedNodeAddr.String(), err.Error())
					}
				}()
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
								p2pool.Server().AddToPeerList(addrPort)
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
								p2pool.Server().AddToPeerList(addrPort)
							}
						}
					}
				}()

				go func() {
					contents := make([]byte, 0, 4096)
					for range time.Tick(time.Minute * 5) {
						contents = contents[:0]
						for _, addrPort := range p2pool.Server().PeerList() {
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

		if err := p2pool.Server().Listen(); err != nil {
			log.Panic(err)
		}
	}
}
