package fetcher

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"arhat.dev/pkg/log"
	"arhat.dev/pkg/tlshelper"

	"github.com/goiiot/libmqtt"
)

func init() {
	RegisterFetcher(MethodMQTT, NewMQTTFetcher)
}

const (
	MethodMQTT = "mqtt"
)

type MQTTConfig struct {
	Broker            string        `json:"broker" yaml:"broker"`
	Transport         string        `json:"transport" yaml:"transport"`
	Username          string        `json:"username" yaml:"username"`
	Password          string        `json:"password" yaml:"password"`
	ClientID          string        `json:"clientID" yaml:"clientID"`
	Version           string        `json:"version" yaml:"version"`
	KeepaliveInterval time.Duration `json:"keepaliveInterval" yaml:"keepaliveInterval"`

	TLS tlshelper.TLSConfig `json:"tls" yaml:"tls"`

	Subscriptions []MQTTSubscriptionConfig `json:"subscriptions" yaml:"subscriptions"`
}

type MQTTSubscriptionConfig struct {
	// Topic of this sub
	Topic string `json:"topic" yaml:"topic"`

	// QoS of this sub
	QoS int `json:"qos" yaml:"qos"`

	// DataKey will be the configmap/secret data key
	DataKey string `json:"dataKey" yaml:"dataKey"`
}

func NewMQTTFetcher(ctx context.Context, logger log.Interface, config *Config) (Interface, error) {
	options := []libmqtt.Option{
		libmqtt.WithBackoffStrategy(time.Second, 10*time.Second, 1.5),
	}

	dataTopics := make(map[string]string)
	var topics []*libmqtt.Topic
	for _, s := range config.MQTT.Subscriptions {
		if s.QoS > 2 || s.QoS < 0 {
			return nil, fmt.Errorf("invalid qos level %q", s.QoS)
		}

		dataTopics[s.Topic] = s.DataKey

		topics = append(topics, &libmqtt.Topic{
			Name: s.Topic,
			Qos:  libmqtt.QosLevel(s.QoS),
		})
	}

	switch config.MQTT.Version {
	case "5":
		options = append(options, libmqtt.WithVersion(libmqtt.V5, false))
	case "3.1.1":
		fallthrough
	default:
		options = append(options, libmqtt.WithVersion(libmqtt.V311, false))
	}

	switch config.MQTT.Transport {
	case "websocket":
		options = append(options, libmqtt.WithWebSocketConnector(0, nil))
	case "tcp":
		fallthrough
	default:
		options = append(options, libmqtt.WithTCPConnector(0))
	}

	keepalive := config.MQTT.KeepaliveInterval
	if keepalive == 0 {
		// default to 60s
		keepalive = 60 * time.Second
	}

	options = append(options, libmqtt.WithConnPacket(libmqtt.ConnPacket{
		CleanSession: true,
		Username:     config.MQTT.Username,
		Password:     config.MQTT.Password,
		ClientID:     config.MQTT.ClientID,
		Keepalive:    uint16(keepalive),
	}))
	options = append(options, libmqtt.WithKeepalive(uint16(float64(keepalive)/float64(time.Second)), 1.2))

	if config.MQTT.TLS.Enabled {
		tlsConfig, err := config.MQTT.TLS.GetTLSConfig(false)
		if err != nil {
			return nil, fmt.Errorf("failed to load tls config: %w", err)
		}
		options = append(options, libmqtt.WithCustomTLS(tlsConfig))
	}

	client, err := libmqtt.NewClient(options...)
	if err != nil {
		return nil, err
	}

	mu := new(sync.RWMutex)
	return &MQTTFetcher{
		log:    logger,
		broker: config.MQTT.Broker,
		topics: topics,
		client: client,

		dataKeys:   config.RequiredDataKeys,
		dataTopics: dataTopics,
		dataBuf:    make(map[string][]byte),
		dataCh:     make(chan map[string][]byte, 1),
		mu:         mu,
		cond:       sync.NewCond(new(sync.Mutex)),
		once:       new(sync.Once),

		connErrCh: make(chan error),
		subErrCh:  make(chan error),
	}, nil
}

var errAlreadySubscribing = fmt.Errorf("already subscribing")

type MQTTFetcher struct {
	log    log.Interface
	broker string
	topics []*libmqtt.Topic
	client libmqtt.Client

	dataKeys   []string
	dataTopics map[string]string
	dataBuf    map[string][]byte
	dataCh     chan map[string][]byte
	mu         *sync.RWMutex
	cond       *sync.Cond
	once       *sync.Once

	subscribing int32
	started     int32

	stopSig   <-chan struct{}
	connErrCh chan error
	subErrCh  chan error
}

// Connect to MQTT broker with connect packet
func (c *MQTTFetcher) Start(stop <-chan struct{}) (err error) {
	c.stopSig = stop

	go c.handleDataUpdated()

	err = c.client.ConnectServer(c.broker,
		libmqtt.WithRouter(libmqtt.NewTextRouter()),
		libmqtt.WithAutoReconnect(true),
		libmqtt.WithConnHandleFunc(c.handleConn),
		libmqtt.WithSubHandleFunc(c.handleSub),
		libmqtt.WithNetHandleFunc(c.handleNet),
	)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = c.Stop()
		}
	}()

	select {
	case <-c.stopSig:
		return context.Canceled
	case err, more := <-c.connErrCh:
		if !more {
			return c.subscribe()
		}

		if err != nil {
			return err
		}
	}

	return c.subscribe()
}

func (c *MQTTFetcher) Retrieve() <-chan map[string][]byte {
	return c.dataCh
}

// Stop mqtt client
func (c *MQTTFetcher) Stop() error {
	c.client.Destroy(false)
	return nil
}

func (c *MQTTFetcher) handleDataUpdated() {
	stopped := func() bool {
		select {
		case <-c.stopSig:
			return true
		default:
			return false
		}
	}

	for !stopped() {
		c.log.V("waiting to be signaled")

		c.cond.L.Lock()
		c.cond.Wait()

		c.log.V("signaled")

		send := func() bool {
			c.mu.RLock()
			defer c.mu.RUnlock()

			for _, k := range c.dataKeys {
				if _, ok := c.dataBuf[k]; !ok {
					// not all data key updated
					c.log.V("data update didn't meet requirement")
					return false
				}
			}

			return true
		}()

		if send {
			c.log.V("sending data update")
			func() {
				c.mu.Lock()
				defer c.mu.Unlock()

				d := c.dataBuf
				select {
				case c.dataCh <- d:
					c.dataBuf = make(map[string][]byte)
				case <-c.stopSig:
					c.log.V("data update not sent due to exited")
					return
				}
			}()
		}

		c.cond.L.Unlock()
	}
}

func (c *MQTTFetcher) handleTopicMsg(client libmqtt.Client, topic string, qos libmqtt.QosLevel, msgBytes []byte) {
	c.log.V("received message", log.String("topic", topic))

	dataKey, ok := c.dataTopics[topic]
	if !ok {
		c.log.D("message ignored", log.String("topic", topic))
		return
	}

	func() {
		c.mu.Lock()
		defer func() {
			c.mu.Unlock()

			c.log.V("signaling update check")
			c.cond.Signal()
		}()

		c.log.V("updating data buffer", log.String("topic", topic), log.String("dataKey", dataKey))

		c.dataBuf[dataKey] = msgBytes
	}()
}

// subscribe to mqtt topics
func (c *MQTTFetcher) subscribe() error {
	c.log.V("subscribing to topics")
	if !atomic.CompareAndSwapInt32(&c.subscribing, 0, 1) {
		return errAlreadySubscribing
	}

	defer func() {
		atomic.StoreInt32(&c.subscribing, 0)
		atomic.StoreInt32(&c.started, 1)
	}()

	for _, t := range c.topics {
		c.client.HandleTopic(t.Name, c.handleTopicMsg)
	}

	c.client.Subscribe(c.topics...)

	select {
	case <-c.stopSig:
		return context.Canceled
	case err := <-c.subErrCh:
		if err != nil {
			return fmt.Errorf("failed to subscribe topics: %w", err)
		}
	}

	return nil
}

func (c *MQTTFetcher) handleNet(client libmqtt.Client, server string, err error) {
	if err != nil {
		if atomic.LoadInt32(&c.subscribing) == 1 && atomic.LoadInt32(&c.started) == 0 {
			select {
			case <-c.stopSig:
				return
			case c.subErrCh <- err:
				// we can close subErrCh here since subscribe has failed
				// and no more action will happen in this client
				close(c.subErrCh)
				return
			}
		}

		if atomic.CompareAndSwapInt32(&c.started, 0, 1) {
			select {
			case <-c.stopSig:
				return
			case c.connErrCh <- err:
				close(c.connErrCh)
				return
			}
		}

		c.log.I("network error happened", log.String("server", server), log.Error(err))
	}
}

func (c *MQTTFetcher) handleConn(client libmqtt.Client, server string, code byte, err error) {
	// nolint:gocritic
	if err != nil {
		if atomic.CompareAndSwapInt32(&c.started, 0, 1) {
			select {
			case <-c.stopSig:
				return
			case c.connErrCh <- err:
				close(c.connErrCh)
				return
			}
		}

		c.log.I("failed to connect to broker", log.Uint8("code", code), log.Error(err))
	} else if code != libmqtt.CodeSuccess {
		if atomic.CompareAndSwapInt32(&c.started, 0, 1) {
			select {
			case <-c.stopSig:
				return
			case c.connErrCh <- fmt.Errorf("rejected by mqtt broker, code: %d", code):
				close(c.connErrCh)
				return
			}
		}

		c.log.I("reconnect rejected by broker", log.Uint8("code", code))
	} else {
		// connection success
		if atomic.LoadInt32(&c.started) == 0 {
			// client still in initial stage
			// close connErrCh to signal connection success
			close(c.connErrCh)
		} else {
			// client has started, connection success means reconnection success
			// so we need to resubscribe topics here
			for {
				if err := c.subscribe(); err != nil {
					if err == errAlreadySubscribing {
						return
					}

					c.log.I("failed to resubscribe to topics after reconnection", log.Error(err))
					time.Sleep(5 * time.Second)
				} else {
					// reject client to cleanup session
					//c.rejectAgent()
					//
					c.log.V("resubscribed to topics after connection lost")
					return
				}
			}
		}
	}
}

func (c *MQTTFetcher) handleSub(client libmqtt.Client, topics []*libmqtt.Topic, err error) {
	if err != nil {
		c.log.I("failed to subscribe", log.Error(err), log.Any("topics", topics))
	} else {
		c.log.D("subscribe succeeded", log.Any("topics", topics))
	}

	select {
	case <-c.stopSig:
		return
	case c.subErrCh <- err:
		return
	}
}
