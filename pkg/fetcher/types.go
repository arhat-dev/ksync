package fetcher

import (
	"context"
	"fmt"
	"sync"

	"arhat.dev/pkg/log"
)

type FactoryFunc func(context.Context, log.Interface, *Config) (Interface, error)

var (
	fetchers = make(map[string]FactoryFunc)
	mu       = new(sync.RWMutex)
)

func RegisterFetcher(name string, factory FactoryFunc) {
	mu.Lock()
	defer mu.Unlock()

	fetchers[name] = factory
}

type Interface interface {
	// Start until stopped by signal
	Start(stop <-chan struct{}) error

	// Retrieve data from remote sources
	Retrieve() <-chan map[string][]byte

	// Stop this fetcher
	Stop() error
}

type Config struct {
	Method string `json:"method" yaml:"method"`

	RequiredDataKeys []string `json:"requiredDataKeys" yaml:"requiredDataKeys"`

	// method specific configuration
	MQTT MQTTConfig `json:"mqtt" yaml:"mqtt"`
}

func New(ctx context.Context, logger log.Interface, config *Config) (Interface, error) {
	mu.RLock()
	defer mu.RUnlock()

	create, ok := fetchers[config.Method]
	if !ok || create == nil {
		return nil, fmt.Errorf("fetcher %q not found", config.Method)
	}

	return create(ctx, logger, config)
}
