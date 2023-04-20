package utils

type urlEntry struct {
	Host  string
	Onion string
}

type siteKey int

const (
	SiteKeyP2PoolIo = siteKey(iota)
	SiteKeyLocalMonero
	SiteKeyExploreMonero
	SiteKeyMoneroCom
	SiteKeyP2PoolObserver
	SiteKeyP2PoolObserverMini
	SiteKeyP2PoolObserverOld
	SiteKeyP2PoolObserverOldMini
	SiteKeyGitGammaspectraLive
	SiteKeyXmrChainNet
	SiteKeySethForPrivacy
	SiteKeyXmrVsBeast
)

var existingUrl = map[siteKey]urlEntry{
	SiteKeyP2PoolIo: {
		Host:  "https://p2pool.io",
		Onion: "http://yucmgsbw7nknw7oi3bkuwudvc657g2xcqahhbjyewazusyytapqo4xid.onion",
	},
	SiteKeyLocalMonero: {
		Host:  "https://localmonero.co",
		Onion: "http://nehdddktmhvqklsnkjqcbpmb63htee2iznpcbs5tgzctipxykpj6yrid.onion",
	},
	SiteKeyExploreMonero: {
		Host: "https://www.exploremonero.com",
	},
	SiteKeyMoneroCom: {
		Host: "https://monero.com",
	},
	SiteKeyP2PoolObserver: {
		Host:  "https://p2pool.observer",
		Onion: "http://p2pool2giz2r5cpqicajwoazjcxkfujxswtk3jolfk2ubilhrkqam2id.onion",
	},
	SiteKeyP2PoolObserverMini: {
		Host:  "https://mini.p2pool.observer",
		Onion: "http://p2pmin25k4ei5bp3l6bpyoap6ogevrc35c3hcfue7zfetjpbhhshxdqd.onion",
	},
	SiteKeyP2PoolObserverOld: {
		Host:  "https://old.p2pool.observer",
		Onion: "http://temp2p7m2ddclcsqx2mrbqrmo7ccixpiu5s2cz2c6erxi2lppptdvxqd.onion",
	},
	SiteKeyP2PoolObserverOldMini: {
		Host:  "https://old-mini.p2pool.observer",
		Onion: "http://temp2pbud6av2jx3lh3yovrj4mjjy2k4p5rxydviosp356ndzs4nd6yd.onion",
	},
	SiteKeyGitGammaspectraLive: {
		Host:  "https://git.gammaspectra.live",
		Onion: "http://gitshn5x75sgs53q3pxwjva2z65ns5vadx3h7u3hrdssbxsova66cxid.onion",
	},
	SiteKeyXmrChainNet: {
		Host:  "https://xmrchain.net",
		Onion: "http://gitshn5x75sgs53q3pxwjva2z65ns5vadx3h7u3hrdssbxsova66cxid.onion",
	},
	SiteKeySethForPrivacy: {
		Host: "https://sethforprivacy.com",
		//Expired Onion: "http://sfprivg7qec6tdle7u6hdepzjibin6fn3ivm6qlwytr235rh5vc6bfqd.onion",
	},
	SiteKeyXmrVsBeast: {
		Host: "https://xmrvsbeast.com",
	},
}

func GetSiteUrl(k siteKey, tryOnion bool) string {
	e := existingUrl[k]
	if tryOnion && e.Onion != "" {
		return e.Onion
	}
	return e.Host
}

func GetSiteUrlByHost(host string, tryOnion bool) string {
	findKey := "https://" + host
	for k, e := range existingUrl {
		if e.Host == findKey {
			return GetSiteUrl(k, tryOnion)
		}
	}
	return ""
}
