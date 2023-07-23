package views

import (
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
)

type GlobalRequestContext struct {
	IsOnion           bool
	DonationAddress   string
	SiteTitle         string
	NetServiceAddress string
	TorServiceAddress string
	Consensus         *sidechain.Consensus
	Pool              *cmdutils.PoolInfoResult
	Socials           struct {
		Irc struct {
			Title   string
			Link    string
			WebChat string
		}
		Matrix struct {
			Link string
		}
	}
	HexBuffer [types.HashSize * 2]byte
}

func (ctx *GlobalRequestContext) GetUrl(host string) string {
	return cmdutils.GetSiteUrlByHost(host, ctx.IsOnion)
}
