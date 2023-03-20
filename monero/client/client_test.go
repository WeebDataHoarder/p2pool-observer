package client

import (
	"git.gammaspectra.live/P2Pool/p2pool-observer/types"
	"os"
	"testing"
)

func init() {
	SetDefaultClientSettings(os.Getenv("MONEROD_RPC_URL"))
}

var txHash, _ = types.HashFromString("d9922a1d03160a16e4704b44dc0ed0e5dffc46db94ca86d6f10545132a0926a0")
var txHashCoinbase, _ = types.HashFromString("dc18b8ad30e15b21c733032288ac0afa08ae51c972b4ee6546ad74aa77c39ebb")

func TestOutputIndexes(t *testing.T) {
	if result, err := GetDefaultClient().GetOutputIndexes(txHashCoinbase); err != nil {
		t.Fatal(err)
	} else {
		t.Log(result)
	}
}

func TestInputs(t *testing.T) {
	if result, err := GetDefaultClient().GetTransactionInputs(txHash); err != nil {
		t.Fatal(err)
	} else {
		t.Log(result)

		inputs := make([]uint64, 0, len(result)*16*128)
		for _, i := range result[0].Inputs {
			inputs = append(inputs, i.KeyOffsets...)
		}

		if result2, err := GetDefaultClient().GetOuts(inputs...); err != nil {
			t.Fatal(err)
		} else {
			t.Log(result2)
		}
	}
}
