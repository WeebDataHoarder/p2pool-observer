package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
	"time"
)

type versionInfo struct {
	Version          string `json:"version"`
	Timestamp        int64  `json:"timestamp"`
	Link             string `json:"link"`
	checkedTimestamp int64
}

type releaseDataJson struct {
	TagName         string    `json:"tag_name"`
	TargetCommitish string    `json:"target_commitish"`
	Name            string    `json:"name"`
	PublishedAt     time.Time `json:"published_at"`
}

var moneroVersion, p2poolVersion versionInfo
var moneroVersionLock, p2poolVersionLock sync.Mutex

const versionCheckInterval = 3600

func getMoneroVersion() versionInfo {
	moneroVersionLock.Lock()
	defer moneroVersionLock.Unlock()
	now := time.Now().Unix()
	if (moneroVersion.checkedTimestamp + versionCheckInterval) >= now {
		return moneroVersion
	}
	response, err := http.DefaultClient.Get("https://api.github.com/repos/monero-project/monero/releases/latest")
	if err != nil {
		return moneroVersion
	}
	defer response.Body.Close()

	var releaseData releaseDataJson
	if data, err := io.ReadAll(response.Body); err != nil {
		return moneroVersion
	} else if err = json.Unmarshal(data, &releaseData); err != nil {
		return moneroVersion
	} else {
		moneroVersion.Version = releaseData.TagName
		moneroVersion.Timestamp = releaseData.PublishedAt.Unix()
		moneroVersion.Link = "https://github.com/monero-project/monero/releases/tag/" + releaseData.TagName
		moneroVersion.checkedTimestamp = now
	}

	return moneroVersion
}

func getP2PoolVersion() versionInfo {
	p2poolVersionLock.Lock()
	defer p2poolVersionLock.Unlock()
	now := time.Now().Unix()
	if (p2poolVersion.checkedTimestamp + versionCheckInterval) >= now {
		return p2poolVersion
	}
	response, err := http.DefaultClient.Get("https://api.github.com/repos/SChernykh/p2pool/releases/latest")
	if err != nil {
		return p2poolVersion
	}
	defer response.Body.Close()

	var releaseData releaseDataJson
	if data, err := io.ReadAll(response.Body); err != nil {
		return p2poolVersion
	} else if err = json.Unmarshal(data, &releaseData); err != nil {
		return p2poolVersion
	} else {
		p2poolVersion.Version = releaseData.TagName
		p2poolVersion.Timestamp = releaseData.PublishedAt.Unix()
		p2poolVersion.Link = "https://github.com/SChernykh/p2pool/releases/tag/" + releaseData.TagName
		p2poolVersion.checkedTimestamp = now
	}

	return p2poolVersion
}
