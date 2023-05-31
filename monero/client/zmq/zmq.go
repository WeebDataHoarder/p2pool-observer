package zmq

import (
	"bytes"
	"context"
	"fmt"
	"git.gammaspectra.live/P2Pool/p2pool-observer/utils"
	"golang.org/x/exp/slices"
	"strings"

	"github.com/go-zeromq/zmq4"
)

type Client struct {
	endpoint string
	topics   []Topic
	sub      zmq4.Socket
}

// NewClient instantiates a new client that will receive monerod's zmq events.
//
//   - `topics` is a list of fully-formed zmq topic to subscribe to
//
//   - `endpoint` is the full address where monerod has been configured to
//     publish the messages to, including the network schama. for instance,
//     considering that monerod has been started with
//
//     monerod --zmq-pub tcp://127.0.0.1:18085
//
//     `endpoint` should be 'tcp://127.0.0.1:18085'.
func NewClient(endpoint string, topics ...Topic) *Client {
	return &Client{
		endpoint: endpoint,
		topics:   topics,
	}
}

// Stream provides channels where instances of the desired topic object are
// sent to.
type Stream struct {
	FullChainMainC    func(*FullChainMain)
	FullTxPoolAddC    func([]FullTxPoolAdd)
	FullMinerDataC    func(*FullMinerData)
	MinimalChainMainC func(*MinimalChainMain)
	MinimalTxPoolAddC func([]TxMempoolData)
}

// Listen listens for a list of topics pre-configured for this client (via NewClient).
func (c *Client) Listen(ctx context.Context, fullChainMain func(chainMain *FullChainMain), fullTxPoolAdd func(txs []FullTxPoolAdd), fullMinerData func(main *FullMinerData), minimalChainMain func(chainMain *MinimalChainMain), minimalTxPoolAdd func(txs []TxMempoolData)) error {
	if err := c.listen(ctx, c.topics...); err != nil {
		return fmt.Errorf("listen on '%s': %w", strings.Join(func() (r []string) {
			for _, s := range c.topics {
				r = append(r, string(s))
			}
			return r
		}(), ", "), err)
	}

	stream := &Stream{
		FullChainMainC:    fullChainMain,
		FullTxPoolAddC:    fullTxPoolAdd,
		FullMinerDataC:    fullMinerData,
		MinimalChainMainC: minimalChainMain,
		MinimalTxPoolAddC: minimalTxPoolAdd,
	}

	if err := c.loop(stream); err != nil {
		return fmt.Errorf("loop: %w", err)
	}

	return nil
}

// Close closes any established connection, if any.
func (c *Client) Close() error {
	if c.sub == nil {
		return nil
	}

	return c.sub.Close()
}

func (c *Client) listen(ctx context.Context, topics ...Topic) error {
	c.sub = zmq4.NewSub(ctx)

	err := c.sub.Dial(c.endpoint)
	if err != nil {
		return fmt.Errorf("dial '%s': %w", c.endpoint, err)
	}

	for _, topic := range topics {
		err = c.sub.SetOption(zmq4.OptionSubscribe, string(topic))
		if err != nil {
			return fmt.Errorf("subscribe: %w", err)
		}
	}

	return nil
}

func (c *Client) loop(stream *Stream) error {
	for {
		msg, err := c.sub.Recv()
		if err != nil {
			return fmt.Errorf("recv: %w", err)
		}

		for _, frame := range msg.Frames {
			err := c.ingestFrameArray(stream, frame)
			if err != nil {
				return fmt.Errorf("consume frame: %w", err)
			}
		}
	}
}

func (c *Client) ingestFrameArray(stream *Stream, frame []byte) error {
	topic, gson, err := jsonFromFrame(frame)
	if err != nil {
		return fmt.Errorf("json from frame: %w", err)
	}

	if slices.Index(c.topics, topic) == -1 {
		return fmt.Errorf("topic '%s' doesn't match "+
			"expected any of '%s'", topic, strings.Join(func() (r []string) {
			for _, s := range c.topics {
				r = append(r, string(s))
			}
			return r
		}(), ", "))
	}

	switch Topic(topic) {
	case TopicFullChainMain:
		return c.transmitFullChainMain(stream, gson)
	case TopicFullTxPoolAdd:
		return c.transmitFullTxPoolAdd(stream, gson)
	case TopicFullMinerData:
		return c.transmitFullMinerData(stream, gson)
	case TopicMinimalChainMain:
		return c.transmitMinimalChainMain(stream, gson)
	case TopicMinimalTxPoolAdd:
		return c.transmitMinimalTxPoolAdd(stream, gson)
	default:
		return fmt.Errorf("unhandled topic '%s'", topic)
	}
}

func (c *Client) transmitFullChainMain(stream *Stream, gson []byte) error {
	var arr []*FullChainMain

	if err := utils.UnmarshalJSON(gson, &arr); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	for _, element := range arr {
		stream.FullChainMainC(element)
	}

	return nil
}

func (c *Client) transmitFullTxPoolAdd(stream *Stream, gson []byte) error {
	var arr []FullTxPoolAdd

	if err := utils.UnmarshalJSON(gson, &arr); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	stream.FullTxPoolAddC(arr)

	return nil
}

func (c *Client) transmitFullMinerData(stream *Stream, gson []byte) error {
	element := &FullMinerData{}

	if err := utils.UnmarshalJSON(gson, element); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	stream.FullMinerDataC(element)
	return nil
}

func (c *Client) transmitMinimalChainMain(stream *Stream, gson []byte) error {
	element := &MinimalChainMain{}

	if err := utils.UnmarshalJSON(gson, element); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	stream.MinimalChainMainC(element)
	return nil
}

func (c *Client) transmitMinimalTxPoolAdd(stream *Stream, gson []byte) error {
	var arr []TxMempoolData

	if err := utils.UnmarshalJSON(gson, &arr); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	stream.MinimalTxPoolAddC(arr)

	return nil
}

func jsonFromFrame(frame []byte) (Topic, []byte, error) {
	unknown := TopicUnknown

	parts := bytes.SplitN(frame, []byte(":"), 2)
	if len(parts) != 2 {
		return unknown, nil, fmt.Errorf(
			"malformed: expected 2 parts, got %d", len(parts))
	}

	topic, gson := string(parts[0]), parts[1]

	switch topic {
	case string(TopicMinimalChainMain):
		return TopicMinimalChainMain, gson, nil
	case string(TopicFullChainMain):
		return TopicFullChainMain, gson, nil
	case string(TopicFullMinerData):
		return TopicFullMinerData, gson, nil
	case string(TopicFullTxPoolAdd):
		return TopicFullTxPoolAdd, gson, nil
	case string(TopicMinimalTxPoolAdd):
		return TopicMinimalTxPoolAdd, gson, nil
	}

	return unknown, nil, fmt.Errorf("unknown topic '%s'", topic)
}
