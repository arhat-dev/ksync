package validator

import (
	"context"
	"fmt"
	"sync"

	"arhat.dev/pkg/log"
)

// variables used when evaluating templates
type templateVars struct {
	DataKeys []string
	DataKey  string
	Data     []byte

	Extra interface{}
}

type FactoryFunc func(context.Context, log.Interface, *Config) (Interface, error)

var (
	validators = make(map[string]FactoryFunc)
	mu         = new(sync.RWMutex)
)

func RegisterValidator(name string, factory FactoryFunc) {
	mu.Lock()
	defer mu.Unlock()

	validators[name] = factory
}

type DataMsg struct {
	// Data is the dataKey to data content map
	Data map[string][]byte

	// Errors happened when retrieving the target data
	// key: <data-key> (for fetcher) or  <fetcher-name>/<data-key> (for syncer)
	Errors map[string]error
}

type Interface interface {
	Validate(data map[string][]byte) *DataMsg
}

// Config for a single validator to validate data
type Config struct {
	// Method is the validator name
	Method string `json:"method" yaml:"method"`

	// DataKeys to get data from newly fetched data
	DataKeys []string `json:"dataKeys" yaml:"dataKeys"`

	// HTTP validator configuration
	HTTP *HTTPConfig `json:"http" yaml:"http"`
	// Text validator configuration
	Text *TextConfig `json:"text" yaml:"text"`
}

func New(ctx context.Context, logger log.Interface, config *Config) (Interface, error) {
	mu.RLock()
	defer mu.RUnlock()

	create, ok := validators[config.Method]
	if !ok || create == nil {
		return nil, fmt.Errorf("validator %q not found", config.Method)
	}

	return create(ctx, logger, config)
}
