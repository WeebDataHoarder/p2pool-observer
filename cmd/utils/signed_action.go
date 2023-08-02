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
	Realm  string              `json:"realm"`
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

func (a *SignedAction) Verify(realm string, addr address.Interface, signature string) address.SignatureVerifyResult {
	if a.Realm != realm {
		// Realm does not match
		return address.ResultFail
	}
	message := a.String()
	return address.VerifyMessage(addr, []byte(message), signature)
}

func (a *SignedAction) VerifyFallbackToZero(realm string, addr address.Interface, signature string) address.SignatureVerifyResult {
	if a.Realm != realm {
		// Realm does not match
		return address.ResultFail
	}
	message := a.String()
	return address.VerifyMessageFallbackToZero(addr, []byte(message), signature)
}

func SignedActionSetMinerAlias(realm, alias string) *SignedAction {
	return &SignedAction{
		Action: "set_miner_alias",
		Data: []SignedActionEntry{
			{
				Key:   "alias",
				Value: alias,
			},
		},
		Realm: realm,
	}
}

func SignedActionUnsetMinerAlias(realm string) *SignedAction {
	return &SignedAction{
		Action: "unset_miner_alias",
		Data:   make([]SignedActionEntry, 0),
		Realm:  realm,
	}
}

func SignedActionAddWebHook(realm, webhookType, webhookUrl string, other ...SignedActionEntry) *SignedAction {
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
		Realm: realm,
	}
}

func SignedActionRemoveWebHook(realm, webhookType, webhookUrlHash string, other ...SignedActionEntry) *SignedAction {
	return &SignedAction{
		Action: "remove_webhook",
		Data: append([]SignedActionEntry{
			{
				Key:   "type",
				Value: webhookType,
			},
			{
				Key:   "url_hash",
				Value: webhookUrlHash,
			},
		}, other...),
		Realm: realm,
	}
}
