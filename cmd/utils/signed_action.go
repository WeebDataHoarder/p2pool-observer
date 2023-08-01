package utils

import (
	"bytes"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/address"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"github.com/goccy/go-json"
	"slices"
)

type SignedActionEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type SignedAction struct {
	Action string              `json:"action"`
	Data   []SignedActionEntry `json:"data"`
}

func (a *SignedAction) Get(key string) (string, bool) {
	if i := slices.IndexFunc(a.Data, func(entry SignedActionEntry) bool {
		return entry.Key == key
	}); i != -1 {
		return a.Data[i].Value, true
	}

	return "", false
}

// String Creates a consistent string for signing
func (a *SignedAction) String() string {
	if a.Data == nil {
		a.Data = make([]SignedActionEntry, 0)
	}
	d, err := utils.MarshalJSON(a)
	if err != nil {
		panic(err)
	}
	buf := bytes.NewBuffer(make([]byte, 0, len(d)))
	if err = json.Compact(buf, d); err != nil {
		panic(err)
	}
	return buf.String()
}

func (a *SignedAction) Verify(addr address.Interface, signature string) address.SignatureVerifyResult {
	message := a.String()
	return address.VerifyMessage(addr, []byte(message), signature)
}

func SignedActionSetMinerAlias(alias string) *SignedAction {
	return &SignedAction{
		Action: "set_miner_alias",
		Data: []SignedActionEntry{
			{
				Key:   "alias",
				Value: alias,
			},
		},
	}
}

func SignedActionUnsetMinerAlias() *SignedAction {
	return &SignedAction{
		Action: "unset_miner_alias",
		Data:   make([]SignedActionEntry, 0),
	}
}

func SignedActionAddWebHook(webhookType, webhookUrl string, other ...SignedActionEntry) *SignedAction {
	return &SignedAction{
		Action: "add_webhook",
		Data: append([]SignedActionEntry{
			{
				Key:   "type",
				Value: webhookType,
			},
			{
				Key:   "url",
				Value: webhookUrl,
			},
		}, other...),
	}
}

func SignedActionRemoveWebHook(webhookType, webhookUrl string, other ...SignedActionEntry) *SignedAction {
	return &SignedAction{
		Action: "remove_webhook",
		Data: append([]SignedActionEntry{
			{
				Key:   "type",
				Value: webhookType,
			},
			{
				Key:   "url",
				Value: webhookUrl,
			},
		}, other...),
	}
}
