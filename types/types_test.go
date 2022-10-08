package types

import "testing"

func TestDifficulty(t *testing.T) {
	hexDiff := "000000000000000000000000683a8b1c"
	diff, err := DifficultyFromString(hexDiff)
	if err != nil {
		t.Fatal(err)
	}

	if diff.String() != hexDiff {
		t.Fatalf("expected %s, got %s", hexDiff, diff)
	}
}
