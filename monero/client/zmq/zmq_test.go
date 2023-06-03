package zmq_test

import (
	"context"
	"errors"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client/zmq"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONFromFrame(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		input         []byte
		expectedJSON  []byte
		expectedTopic zmq.Topic
		err           string
	}{
		{
			name:  "nil",
			input: nil,
			err:   "malformed",
		},

		{
			name:  "empty",
			input: []byte{},
			err:   "malformed",
		},

		{
			name:  "unknown-topic",
			input: []byte(`foobar:[{"foo":"bar"}]`),
			err:   "unknown topic",
		},

		{
			name:          "proper w/ known-topic",
			input:         []byte(`json-minimal-txpool_add:[{"foo":"bar"}]`),
			expectedTopic: zmq.TopicMinimalTxPoolAdd,
			expectedJSON:  []byte(`[{"foo":"bar"}]`),
		},
	} {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			aTopic, aJSON, err := zmq.JSONFromFrame(tc.input)
			if tc.err != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.err)
				return
			}

			assert.NoError(t, err)
			assert.Equal(t, tc.expectedTopic, aTopic)
			assert.Equal(t, tc.expectedJSON, aJSON)
		})
	}
}

func TestClient(t *testing.T) {
	client := zmq.NewClient(os.Getenv("MONEROD_ZMQ_URL"), zmq.TopicFullChainMain, zmq.TopicFullTxPoolAdd, zmq.TopicFullMinerData, zmq.TopicMinimalChainMain, zmq.TopicMinimalTxPoolAdd)
	ctx, ctxFunc := context.WithTimeout(context.Background(), time.Second*10)
	defer ctxFunc()
	err := client.Listen(ctx, func(chainMain *zmq.FullChainMain) {
		t.Log(chainMain)
	}, func(txs []zmq.FullTxPoolAdd) {
		t.Log(txs)
	}, func(main *zmq.FullMinerData) {
		t.Log(main)
	}, func(chainMain *zmq.MinimalChainMain) {
		t.Log(chainMain)
	}, func(txs []zmq.TxMempoolData) {

		t.Log(txs)
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
