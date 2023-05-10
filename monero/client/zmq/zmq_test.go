package zmq_test

import (
	"context"
	"git.gammaspectra.live/P2Pool/p2pool-observer/monero/client/zmq"
	"log"
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
	ctx, ctxFunc := context.WithTimeout(context.Background(), time.Second*30)
	defer ctxFunc()
	s, err := client.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}

	for {
		select {
		case err := <-s.ErrC:
			if err == context.DeadlineExceeded {
				break
			} else {
				t.Fatal(err)
			}
			return
		case fullChainMain := <-s.FullChainMainC:
			log.Print(fullChainMain)
		case fullTxPoolAdd := <-s.FullTxPoolAddC:
			log.Print(fullTxPoolAdd)
		case fullMinerData := <-s.FullMinerDataC:
			log.Print(fullMinerData)
		case minimalChainMain := <-s.MinimalChainMainC:
			log.Print(minimalChainMain)
		case minimalTxPoolAdd := <-s.MinimalTxPoolAddC:
			log.Print(minimalTxPoolAdd)
		}
	}
}
