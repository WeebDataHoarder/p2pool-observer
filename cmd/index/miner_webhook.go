package index

import (
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/sidechain"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"net/url"
	"slices"
	"strings"
)

type MinerWebHook struct {
	Miner     uint64               `json:"miner"`
	Type      WebHookType          `json:"type"`
	Url       string               `json:"url"`
	Settings  map[string]string    `json:"settings"`
	Consensus *sidechain.Consensus `json:"-"`
}

type WebHookType string

const (
	WebHookSlack          WebHookType = "slack"
	WebHookDiscord        WebHookType = "discord"
	WebHookTelegram       WebHookType = "telegram"
	WebHookMatrixHookshot WebHookType = "matrix-hookshot"
	WebHookCustom         WebHookType = "custom"
)

var disallowedCustomHookHosts = []string{
	"hooks.slack.com",
	"discord.com",
	"api.telegram.org",
}

var disallowedCustomHookPorts = []string{}

func (w *MinerWebHook) ScanFromRow(consensus *sidechain.Consensus, row RowScanInterface) error {
	var settingsBuf []byte

	w.Consensus = consensus
	w.Settings = make(map[string]string)

	if err := row.Scan(&w.Miner, &w.Type, &w.Url, &settingsBuf); err != nil {
		return err
	} else if err = utils.UnmarshalJSON(settingsBuf, &w.Settings); err != nil {
		return err
	}
	return nil
}

func (w *MinerWebHook) Verify() error {
	uri, err := url.Parse(w.Url)
	if err != nil {
		return err
	}
	switch w.Type {
	case WebHookSlack:
		if uri.Scheme != "https" {
			return errors.New("invalid URL scheme, expected https")
		}
		if uri.Host != "hooks.slack.com" {
			return errors.New("invalid hook host, expected hooks.slack.com")
		}
		if uri.Port() != "" {
			return errors.New("unexpected port")
		}
		if !strings.HasPrefix(uri.Path, "/services/") {
			return errors.New("invalid hook path start")
		}
	case WebHookDiscord:
		if uri.Scheme != "https" {
			return errors.New("invalid URL scheme, expected https")
		}
		if uri.Host != "discord.com" {
			return errors.New("invalid hook host, expected discord.com")
		}
		if uri.Port() != "" {
			return errors.New("unexpected port")
		}
		if !strings.HasPrefix(uri.Path, "/api/webhooks/") {
			return errors.New("invalid hook path start")
		}
	case WebHookTelegram:
		if uri.Scheme != "https" {
			return errors.New("invalid URL scheme, expected https")
		}
		if uri.Host != "api.telegram.org" {
			return errors.New("invalid hook host, expected api.telegram.org")
		}
		if uri.Port() != "" {
			return errors.New("unexpected port")
		}
		if !strings.HasPrefix(uri.Path, "/bot") {
			return errors.New("invalid hook path start")
		}
		if !strings.HasSuffix(uri.Path, "/sendMessage") {
			return errors.New("invalid hook path end")
		}
	case WebHookMatrixHookshot:
		if uri.Scheme != "https" {
			return errors.New("invalid URL scheme, expected https")
		}
		if slices.Contains(disallowedCustomHookHosts, strings.ToLower(uri.Hostname())) {
			return errors.New("disallowed hook host")
		}
		if uri.Port() != "" {
			return errors.New("unexpected port")
		}
	case WebHookCustom:
		if uri.Scheme != "https" && uri.Scheme != "http" {
			return errors.New("invalid URL scheme, expected http or https")
		}
		if slices.Contains(disallowedCustomHookHosts, strings.ToLower(uri.Hostname())) {
			return errors.New("disallowed hook host")
		}
		if slices.Contains(disallowedCustomHookPorts, strings.ToLower(uri.Port())) {
			return errors.New("disallowed hook port")
		}
	default:
		return errors.New("unsupported hook type")
	}
	return nil
}
