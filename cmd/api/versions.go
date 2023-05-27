package main

import (
	cmdutils "git.gammaspectra.live/P2Pool/p2pool-observer/cmd/utils"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
	"net/http"
	"sync"
	"time"
)

var moneroVersion, p2poolVersion cmdutils.VersionInfo
var moneroVersionLock, p2poolVersionLock sync.Mutex

const versionCheckInterval = 3600

func getMoneroVersion() cmdutils.VersionInfo {
	moneroVersionLock.Lock()
	defer moneroVersionLock.Unlock()
	now := time.Now().Unix()
	if (moneroVersion.CheckedTimestamp + versionCheckInterval) >= now {
		return moneroVersion
	}
	response, err := http.DefaultClient.Get("https://api.github.com/repos/monero-project/monero/releases/latest")
	if err != nil {
		return moneroVersion
	}
	defer response.Body.Close()

	var releaseData cmdutils.ReleaseDataJson
	if data, err := io.ReadAll(response.Body); err != nil {
		return moneroVersion
	} else if err = utils.UnmarshalJSON(data, &releaseData); err != nil {
		return moneroVersion
	} else {
		moneroVersion.Version = releaseData.TagName
		moneroVersion.Timestamp = releaseData.PublishedAt.Unix()
		moneroVersion.Link = "https://github.com/monero-project/monero/releases/tag/" + releaseData.TagName
		moneroVersion.CheckedTimestamp = now
	}

	return moneroVersion
}

func getP2PoolVersion() cmdutils.VersionInfo {
	p2poolVersionLock.Lock()
	defer p2poolVersionLock.Unlock()
	now := time.Now().Unix()
	if (p2poolVersion.CheckedTimestamp + versionCheckInterval) >= now {
		return p2poolVersion
	}
	response, err := http.DefaultClient.Get("https://api.github.com/repos/SChernykh/p2pool/releases/latest")
	if err != nil {
		return p2poolVersion
	}
	defer response.Body.Close()

	var releaseData cmdutils.ReleaseDataJson
	if data, err := io.ReadAll(response.Body); err != nil {
		return p2poolVersion
	} else if err = utils.UnmarshalJSON(data, &releaseData); err != nil {
		return p2poolVersion
	} else {
		p2poolVersion.Version = releaseData.TagName
		p2poolVersion.Timestamp = releaseData.PublishedAt.Unix()
		p2poolVersion.Link = "https://github.com/SChernykh/p2pool/releases/tag/" + releaseData.TagName
		p2poolVersion.CheckedTimestamp = now
	}

	return p2poolVersion
}
