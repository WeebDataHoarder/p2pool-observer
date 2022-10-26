package sidechain

import "testing"

func TestConsensusId(t *testing.T) {
	id := ConsensusMini.CalculateId()
	if id != ConsensusMini.Id() {
		t.Fatalf("wrong mini sidechain id, expected %s, got %s", ConsensusMini.Id().String(), id.String())
	}

	id = ConsensusDefault.CalculateId()
	if id != ConsensusDefault.Id() {
		t.Fatalf("wrong default sidechain id, expected %s, got %s", ConsensusDefault.Id().String(), id.String())
	}
}
