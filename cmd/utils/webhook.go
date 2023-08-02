package utils

import (
	"bytes"
	"errors"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/cmd/index"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/p2pool/types"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"
)

type JSONWebHookDiscord struct {
	Username        string                           `json:"username,omitempty"`
	AvatarUrl       string                           `json:"avatar_url,omitempty"`
	Content         string                           `json:"content,omitempty"`
	Embeds          []JSONWebHookDiscordEmbed        `json:"embeds,omitempty"`
	Components      []JSONWebHookDiscordComponentRow `json:"components,omitempty"`
	TTS             bool                             `json:"tts,omitempty"`
	AllowedMentions *struct {
		Parse []string `json:"parse,omitempty"`
		Users []string `json:"users,omitempty"`
		Roles []string `json:"roles,omitempty"`
	} `json:"allowed_mentions,omitempty"`
}

func NewJSONWebHookDiscordComponent(b ...JSONWebHookDiscordComponentButton) JSONWebHookDiscordComponentRow {
	return JSONWebHookDiscordComponentRow{
		Type:       1,
		Components: b,
	}
}

func NewJSONWebHookDiscordComponentButton(label, url string) JSONWebHookDiscordComponentButton {
	return JSONWebHookDiscordComponentButton{
		Type:  2,
		Style: 5,
		Label: label,
		Url:   url,
	}
}

type JSONWebHookDiscordComponentRow struct {
	Type       int                                 `json:"type"`
	Components []JSONWebHookDiscordComponentButton `json:"components"`
}

type JSONWebHookDiscordComponentButton struct {
	Type  int    `json:"type"`
	Style int    `json:"style"`
	Label string `json:"label,omitempty"`
	Url   string `json:"url,omitempty"`
}

type JSONWebHookDiscordEmbed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Url         string `json:"url,omitempty"`
	// Timestamp Time in time.RFC3339 YYYY-MM-DDTHH:MM:SS.MSSZ format
	Timestamp string  `json:"timestamp,omitempty"`
	Color     *uint64 `json:"color,omitempty"`

	Footer *struct {
		Text string `json:"text,omitempty"`
	} `json:"footer,omitempty"`

	Image *struct {
		Url string `json:"url,omitempty"`
	} `json:"image,omitempty"`
	Thumbnail *struct {
		Url string `json:"url,omitempty"`
	} `json:"thumbnail,omitempty"`

	Provider *struct {
		Name    string `json:"name,omitempty"`
		IconUrl string `json:"icon_url,omitempty"`
	} `json:"provider,omitempty"`

	Author *struct {
		Name    string `json:"name,omitempty"`
		Url     string `json:"url,omitempty"`
		IconUrl string `json:"icon_url,omitempty"`
	} `json:"author,omitempty"`

	Fields []JSONWebHookDiscordEmbedField `json:"fields,omitempty"`
}

type JSONWebHookDiscordEmbedField struct {
	Name   string `json:"name,omitempty"`
	Value  string `json:"value,omitempty"`
	Inline bool   `json:"inline,omitempty"`
}

var (
	DiscordColorGreen     uint64 = 5763719
	DiscordColorDarkGreen uint64 = 2067276
	DiscordColorOrange    uint64 = 15105570
)

var WebHookRateLimit = time.NewTicker(time.Second / 20)
var WebHookUserAgent string

var WebHookHost string

var WebHookVersion = fmt.Sprintf("%d.%d", types.CurrentSoftwareVersion.Major(), types.CurrentSoftwareVersion.Minor())

func init() {
	WebHookHost = os.Getenv("NET_SERVICE_ADDRESS")
	WebHookUserAgent = fmt.Sprintf("Mozilla/5.0 (compatible;GoObserver %s; +https://%s/api#webhooks)", types.CurrentSoftwareVersion.String(), WebHookHost)
}

var WebHookClient = http.Client{
	Timeout: 10 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func SetWebHookProxy(hostPort string) {
	WebHookClient.Transport = &http.Transport{
		Proxy: http.ProxyURL(&url.URL{
			Scheme: "socks5",
			Host:   hostPort,
		}),
	}
}

func SendSideBlock(w *index.MinerWebHook, ts int64, source *address.Address, block *index.SideBlock) error {
	if !CanSendToHook(w, "side_blocks") {
		return nil
	}

	blockValuation := func() string {
		if block.IsOrphan() {
			return "0%"
		} else if block.IsUncle() {
			return strconv.FormatUint(100-w.Consensus.UnclePenalty, 10) + "% (uncle)"
		} else if block.IsUncle() {
			return "100% + " + strconv.FormatUint(w.Consensus.UnclePenalty, 10) + "% of " + strconv.FormatUint(uint64(len(block.Uncles)), 10) + "% uncle(s)"
		} else {
			return "100%"
		}
	}

	blockWeight, _ := block.Weight(block.EffectiveHeight, w.Consensus.ChainWindowSize, w.Consensus.UnclePenalty)

	switch w.Type {
	case index.WebHookDiscord:

		var color = &DiscordColorGreen
		fields := []JSONWebHookDiscordEmbedField{
			{
				Name:   "Pool Height",
				Value:  strconv.FormatUint(block.SideHeight, 10),
				Inline: true,
			},
		}
		if block.IsUncle() {
			color = &DiscordColorDarkGreen
			fields = append(fields, JSONWebHookDiscordEmbedField{
				Name:   "Parent Height",
				Value:  "[" + strconv.FormatUint(block.EffectiveHeight, 10) + "](" + "https://" + WebHookHost + "/share/" + block.UncleOf.String() + ")",
				Inline: true,
			})
		} else {
			fields = append(fields, JSONWebHookDiscordEmbedField{
				Name:   "Monero Height",
				Value:  strconv.FormatUint(block.MainHeight, 10),
				Inline: true,
			})
		}
		fields = append(fields,
			JSONWebHookDiscordEmbedField{
				Name:  "Found by",
				Value: "[`" + string(utils.ShortenSlice(block.MinerAddress.ToBase58(), 24)) + "`](" + "https://" + WebHookHost + "/miner/" + string(block.MinerAddress.ToBase58()) + ")",
			},
			JSONWebHookDiscordEmbedField{
				Name:  "Template Id",
				Value: "[`" + block.TemplateId.String() + "`](" + "https://" + WebHookHost + "/share/" + block.TemplateId.String() + ")",
			},
			JSONWebHookDiscordEmbedField{
				Name:   "Valuation",
				Value:  blockValuation(),
				Inline: true,
			},
			JSONWebHookDiscordEmbedField{
				Name:   "Weight",
				Value:  utils.SiUnits(float64(blockWeight), 4),
				Inline: true,
			},
		)

		return SendJsonPost(w, ts, source, &JSONWebHookDiscord{
			Embeds: []JSONWebHookDiscordEmbed{
				{
					Color: color,
					Title: "**" + func() string {
						if block.IsUncle() {
							return "UNCLE "
						} else {
							return ""
						}
					}() + "SHARE FOUND** on P2Pool " + func() string {
						if w.Consensus.IsDefault() {
							return "Main"
						} else if w.Consensus.IsMini() {
							return "Mini"
						} else {
							return "Unknown"
						}
					}(),
					Url:         "https://" + WebHookHost + "/share/" + block.MainId.String(),
					Description: "",
					Fields:      fields,
					Footer: &struct {
						Text string `json:"text,omitempty"`
					}{
						Text: "Share mined using " + block.SoftwareId.String() + " " + block.SoftwareVersion.String(),
					},
					Timestamp: time.Unix(int64(block.Timestamp), 0).UTC().Format(time.RFC3339),
				},
			},
		})
	case index.WebHookCustom:
		return SendJsonPost(w, ts, source, &JSONEvent{
			Type:      JSONEventSideBlock,
			SideBlock: block,
		})
	default:
		return errors.New("unsupported hook type")
	}
}

func SendFoundBlock(w *index.MinerWebHook, ts int64, source *address.Address, block *index.FoundBlock, outputs index.MainCoinbaseOutputs) error {
	if !CanSendToHook(w, "found_blocks") {
		return nil
	}

	switch w.Type {
	case index.WebHookCustom:
		return SendJsonPost(w, ts, source, &JSONEvent{
			Type:                JSONEventFoundBlock,
			FoundBlock:          block,
			MainCoinbaseOutputs: outputs,
		})
	default:
		return errors.New("unsupported hook type")
	}
}

func CanSendToHook(w *index.MinerWebHook, key string) bool {
	if v, ok := w.Settings["send_"+key]; ok && v == "false" {
		return false
	}
	return true
}

func SendPayout(w *index.MinerWebHook, ts int64, source *address.Address, payout *index.Payout) error {
	if !CanSendToHook(w, "payouts") {
		return nil
	}

	switch w.Type {
	case index.WebHookDiscord:
		return SendJsonPost(w, ts, source, &JSONWebHookDiscord{
			Embeds: []JSONWebHookDiscordEmbed{
				{
					Color: &DiscordColorOrange,
					Title: "**RECEIVED PAYOUT** on P2Pool " + func() string {
						if w.Consensus.IsDefault() {
							return "Main"
						} else if w.Consensus.IsMini() {
							return "Mini"
						} else {
							return "Unknown"
						}
					}(),
					Url:         "https://" + WebHookHost + "/proof/" + payout.MainId.String() + "/" + strconv.FormatUint(payout.Index, 10),
					Description: "",
					Fields: []JSONWebHookDiscordEmbedField{
						{
							Name:   "Pool Height",
							Value:  strconv.FormatUint(payout.SideHeight, 10),
							Inline: true,
						},
						{
							Name:   "Monero Height",
							Value:  strconv.FormatUint(payout.MainHeight, 10),
							Inline: true,
						},
						{
							Name:  "Monero Id",
							Value: "[`" + payout.MainId.String() + "`](" + GetSiteUrl(SiteKeyP2PoolIo, false) + "/explorer/block/" + payout.MainId.String() + ")",
						},
						{
							Name:  "Template Id",
							Value: "[`" + payout.TemplateId.String() + "`](" + "https://" + WebHookHost + "/share/" + payout.TemplateId.String() + ")",
						},

						{
							Name:  "Coinbase Id",
							Value: "[`" + payout.CoinbaseId.String() + "`](" + GetSiteUrl(SiteKeyP2PoolIo, false) + "/explorer/tx/" + payout.CoinbaseId.String() + ")",
						},
						{
							Name:  "Coinbase Private Key",
							Value: "[`" + payout.PrivateKey.String() + "`](" + "https://" + WebHookHost + "/proof/" + payout.MainId.String() + "/" + strconv.FormatUint(payout.Index, 10) + ")",
						},
						{
							Name:  "Payout address",
							Value: "[`" + string(utils.ShortenSlice(source.ToBase58(), 24)) + "`](" + "https://" + WebHookHost + "/miner/" + string(source.ToBase58()) + ")",
						},
						{
							Name:   "Reward",
							Value:  "**" + utils.XMRUnits(payout.Reward) + " XMR" + "**",
							Inline: true,
						},
						{
							Name:   "Global Output Index",
							Value:  strconv.FormatUint(payout.GlobalOutputIndex, 10),
							Inline: true,
						},
					},
					Timestamp: time.Unix(int64(payout.Timestamp), 0).UTC().Format(time.RFC3339),
				},
			},
		})
	case index.WebHookCustom:
		return SendJsonPost(w, ts, source, &JSONEvent{
			Type:   JSONEventPayout,
			Payout: payout,
		})
	default:
		return errors.New("unsupported hook type")
	}
}

func SendOrphanedBlock(w *index.MinerWebHook, ts int64, source *address.Address, block *index.SideBlock) error {
	if !CanSendToHook(w, "orphaned_blocks") {
		return nil
	}

	switch w.Type {
	case index.WebHookCustom:
		return SendJsonPost(w, ts, source, &JSONEvent{
			Type:      JSONEventOrphanedBlock,
			SideBlock: block,
		})
	default:
		return errors.New("unsupported hook type")
	}
}

func SendJsonPost(w *index.MinerWebHook, ts int64, source *address.Address, data any) error {
	uri, err := url.Parse(w.Url)
	if err != nil {
		return err
	}

	body, err := utils.MarshalJSON(data)
	if err != nil {
		return err
	}

	headers := make(http.Header)
	headers.Set("Accept", "*/*")
	headers.Set("User-Agent", WebHookUserAgent)
	headers.Set("Content-Type", "application/json")
	headers.Set("X-P2Pool-Observer-Timestamp", strconv.FormatInt(ts, 10))
	headers.Set("X-P2Pool-Observer-Version", WebHookVersion)
	headers.Set("X-P2Pool-Observer-Host", WebHookHost)
	headers.Set("X-P2Pool-Observer-Consensus-ID", w.Consensus.Id.String())
	headers.Set("X-P2Pool-Observer-Address", string(source.ToBase58()))

	// apply rate limit
	<-WebHookRateLimit.C

	response, err := WebHookClient.Do(&http.Request{
		Method:        "POST",
		URL:           uri,
		Header:        headers,
		Body:          io.NopCloser(bytes.NewBuffer(body)),
		ContentLength: int64(len(body)),
	})
	if err != nil {
		return err
	}
	defer response.Body.Close()
	defer io.ReadAll(response.Body)

	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusNoContent {
		data, _ := io.ReadAll(response.Body)
		return fmt.Errorf("error code %d: %s", response.StatusCode, string(data))
	}
	return nil
}
