package crypto

import "testing"

func TestHash(t *testing.T) {
	h := PooledKeccak256([]byte("test")).String()

	if h != "9c22ff5f21f0b81b113e63f7db6da94fedef11b2119b4088b89664fb9a3cb658" {
		t.Fatalf("got %s, expected %s", h, "9c22ff5f21f0b81b113e63f7db6da94fedef11b2119b4088b89664fb9a3cb658")
	}
}
