package p2pool

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"net/netip"
	"os"
	"testing"
)

func TestClient(t *testing.T) {
	client.SetClientSettings(os.Getenv("MONEROD_RPC_URL"))
	settings := make(map[string]string)
	settings["listen"] = "127.0.0.1:39889"
	if p2pool := NewP2Pool(sidechain.ConsensusDefault, settings); p2pool == nil {
		t.Fatal()
	} else {
		if err := p2pool.server.Connect(netip.MustParseAddrPort("127.0.0.1:37889")); err != nil {
			t.Fatal(err)
		}

		t.Fatal(p2pool.Server().Listen())
	}
}
