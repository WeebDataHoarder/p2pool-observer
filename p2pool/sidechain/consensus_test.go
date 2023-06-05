package sidechain

import (
	"strings"
	"testing"
)

func TestDefaultConsensusId(t *testing.T) {
	id := ConsensusMini.CalculateId()
	if id != ConsensusMini.Id {
		t.Fatalf("wrong mini sidechain id, expected %s, got %s", ConsensusMini.Id.String(), id.String())
	}

	id = ConsensusDefault.CalculateId()
	if id != ConsensusDefault.Id {
		t.Fatalf("wrong default sidechain id, expected %s, got %s", ConsensusDefault.Id.String(), id.String())
	}
}

func TestOverlyLongConsensus(t *testing.T) {

	c := NewConsensus(NetworkMainnet, strings.Repeat("A", 128), strings.Repeat("A", 128), 10, 100000, 2160, 20)

	c2 := NewConsensus(NetworkMainnet, strings.Repeat("A", 128), strings.Repeat("A", 128), 100, 1000000, 1000, 30)

	if c.Id == c2.Id {
		t.Fatalf("consensus is different but ids are equal, %s, %s", c.Id.String(), c2.Id.String())
	}
}
