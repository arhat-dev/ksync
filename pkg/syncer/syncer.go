package syncer

import (
	"context"
	"fmt"
	"sync"

	"arhat.dev/pkg/log"
	"go.uber.org/multierr"

	"arhat.dev/ksync/pkg/fetcher"
	"arhat.dev/ksync/pkg/validator"
)

type Config struct {
	RequiredDataKeys []string            `json:"requiredDataKeys" yaml:"requiredDataKeys"`
	Fetchers         []*fetcher.Config   `json:"fetchers" yaml:"fetchers"`
	Validators       []*validator.Config `json:"validators" yaml:"validators"`
}

func NewSyncer(ctx context.Context, logger log.Interface, config *Config) (*Syncer, error) {
	mu := new(sync.RWMutex)
	ctx, exit := context.WithCancel(ctx)
	_ = exit

	var fetchers []fetcher.Interface
	for i, fc := range config.Fetchers {
		logger.V(fmt.Sprintf("creating fetcher %d, method %q", i, fc.Method))
		f, err := fetcher.New(ctx, logger, fc)
		if err != nil {
			return nil, fmt.Errorf("failed to create fetcher: %w", err)
		}
		fetchers = append(fetchers, f)
	}

	var validators []validator.Interface
	for i, vc := range config.Validators {
		logger.V(fmt.Sprintf("creating validator %d, method %q", i, vc.Method))
		v, err := validator.New(ctx, logger, vc)
		if err != nil {
			return nil, fmt.Errorf("failed to create validator: %w", err)
		}
		validators = append(validators, v)
	}

	s := &Syncer{
		ctx:  ctx,
		exit: exit,

		logger:     logger,
		fetchers:   fetchers,
		validators: validators,

		dataKeys: config.RequiredDataKeys,
		dataBuf:  make(map[string][]byte),
		mu:       mu,
		cond:     sync.NewCond(new(sync.Mutex)),
		stopped:  false,
		dataCh:   make(chan map[string][]byte),
	}

	return s, nil
}

// Syncer is the combination of fetchers and validators
type Syncer struct {
	ctx  context.Context
	exit context.CancelFunc

	logger     log.Interface
	fetchers   []fetcher.Interface
	validators []validator.Interface

	dataKeys []string
	dataBuf  map[string][]byte
	mu       *sync.RWMutex
	cond     *sync.Cond
	stopped  bool
	dataCh   chan map[string][]byte
}

func (s *Syncer) Start(stop <-chan struct{}) (err error) {
	s.logger.D("starting syncer")
	defer func() {
		if err != nil {
			_ = s.Stop()
		}
	}()

	for i, f := range s.fetchers {
		s.logger.V("starting fetcher for syncer")
		if err = f.Start(stop); err != nil {
			return fmt.Errorf("failed to start fetcher %d: %w", i, err)
		}
	}

	for i := range s.fetchers {
		go s.handleDataRetrievedFromFetcher(s.fetchers[i].Retrieve())
	}

	go s.handleDataUpdated()

	return nil
}

func (s *Syncer) Retrieve() <-chan map[string][]byte {
	return s.dataCh
}

func (s *Syncer) Stop() (err error) {
	select {
	case <-s.ctx.Done():
		return
	default:
	}

	s.logger.V("stopping syncer")
	for _, f := range s.fetchers {
		if e := f.Stop(); e != nil {
			err = multierr.Append(err, e)
		}
	}

	s.exit()

	s.mu.Lock()
	defer s.mu.Unlock()

	s.cond.Broadcast()

	close(s.dataCh)

	return err
}

func (s *Syncer) handleDataUpdated() {
	stopped := func() bool {
		select {
		case <-s.ctx.Done():
			return true
		default:
			return false
		}
	}

	for !stopped() {
		s.logger.V("waiting to be signaled")

		s.cond.L.Lock()
		s.cond.Wait()

		s.logger.V("signaled")

		send := func() bool {
			s.mu.RLock()
			defer s.mu.RUnlock()

			for _, k := range s.dataKeys {
				if _, ok := s.dataBuf[k]; !ok {
					// not all data key updated
					return false
				}
			}

			return !stopped()
		}()

		if send {
			func() {
				s.mu.Lock()
				defer s.mu.Unlock()

				d := s.dataBuf
				select {
				case <-s.ctx.Done():
					return
				case s.dataCh <- d:
					s.dataBuf = make(map[string][]byte)
				}
			}()
		}

		s.cond.L.Unlock()
	}
}

func (s *Syncer) handleDataRetrievedFromFetcher(ch <-chan map[string][]byte) {
	for msg := range ch {
		data := msg

		for i, v := range s.validators {
			s.logger.V(fmt.Sprintf("validating with validator %d", i))
			data = s.processData(v, data)
		}

		func() {
			s.mu.Lock()
			defer func() {
				s.mu.Unlock()

				s.cond.Signal()
			}()

			for k, v := range data {
				s.dataBuf[k] = v
			}
		}()
	}
}

func (s *Syncer) processData(p validator.Interface, data map[string][]byte) map[string][]byte {
	dataMsg := p.Validate(data)

	for k, v := range dataMsg.Data {
		data[k] = v
		s.logger.V(fmt.Sprintf("data for key %q is valid", k))
	}

	for k, v := range dataMsg.Errors {
		s.logger.I(fmt.Sprintf("data for key %q not valid", k), log.Error(v))
		delete(data, k)
	}

	return data
}
