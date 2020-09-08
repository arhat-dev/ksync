package controller

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"

	"arhat.dev/pkg/log"
	"arhat.dev/pkg/reconcile"
	"go.uber.org/multierr"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"arhat.dev/ksync/pkg/constant"
	"arhat.dev/ksync/pkg/syncer"
)

func (c *Controller) handleSyncerConfigUpdate(obj interface{}) *reconcile.Result {
	logger := c.logger.WithFields(log.String("action", "syncer-update"))
	spec, ok := obj.(*syncerSpec)
	if !ok {
		logger.I("invalid key, not a syncerSpec", log.Any("keyType", reflect.TypeOf(obj).String()))
		return nil
	}

	// stop it
	if spec.syncer != nil {
		logger.V("stopping old syncer")
		err := spec.syncer.Stop()
		if err != nil {
			logger.I("syncer stopped with error", log.Error(err))
		}
	} else {
		logger.I("nil syncer provided for update")
	}

	// remove it (should not fail)
	logger.V("removing old syncer")
	err := c.removeSyncer(nil, &spec.syncerConfig)
	if err != nil {
		logger.I("failed to remove syncer for update", log.Error(err))
		return &reconcile.Result{Err: err}
	}

	// create it
	logger.V("ensuring syncer")
	_, err = c.ensureSyncer(spec.targetConfig, nil, &spec.syncerConfig)
	if err != nil {
		logger.I("failed to ensure new syncer", log.Error(err))
		return &reconcile.Result{Err: err}
	}

	return nil
}

func (c *Controller) removeSyncer(md metav1.ObjectMetaAccessor, trigger *configRef) error {
	if md == nil && trigger == nil {
		return fmt.Errorf("unable to create key for syncer, none of metadata and trigger provided")
	}

	if trigger == nil {
		var err error
		trigger, err = createTriggerForSyncerConfigResourceFromMetadata(md)
		if err != nil {
			return fmt.Errorf("failed to create trigger for the syncer config: %w", err)
		}
	}

	c.syncerMu.Lock()
	defer c.syncerMu.Unlock()

	spec, ok := c.syncerTriggerIndex[*trigger]
	if !ok {
		return nil
	}

	if spec.syncer != nil {
		_ = spec.syncer.Stop()
	}

	delete(c.syncerTriggerIndex, *trigger)

	return nil
}

func createTriggerForSyncerConfigResourceFromMetadata(md metav1.ObjectMetaAccessor) (*configRef, error) {
	annotations := md.GetObjectMeta().GetAnnotations()

	if len(annotations) == 0 {
		return nil, fmt.Errorf("no annotation found")
	}

	link, ok := annotations[constant.AnnotationSyncConfig]
	if !ok || link == "" {
		return nil, fmt.Errorf("no sync config annotation found")
	}

	u, err := url.Parse(link)
	if err != nil {
		return nil, fmt.Errorf("invalid sync config link: %w", err)
	}

	trigger := new(configRef)

	target := filepath.Clean(filepath.Join(u.Host, u.Path))
	parts := strings.SplitN(target, "/", 3)

	switch len(parts) {
	case 2:
		// <name>/<key> -> namespace is the namespace we found this config
		trigger.namespace = md.GetObjectMeta().GetNamespace()
		trigger.name = parts[0]
		trigger.key = parts[1]
	case 3:
		// <namespace>/<name>/<key>
		trigger.namespace = parts[0]
		trigger.name = parts[1]
		trigger.key = parts[2]
	default:
		return nil, fmt.Errorf("invalid sync config reference %q", target)
	}

	switch u.Scheme {
	case "configmap":
		trigger.kind = configKindCM
	case "secret":
		trigger.kind = configKindSecret
	default:
		return nil, fmt.Errorf("unsupported sync config link scheme %q", u.Scheme)
	}

	return trigger, nil
}

func (c *Controller) ensureSyncer(
	syncTarget configRef,
	md metav1.ObjectMetaAccessor,
	trigger *configRef,
) (bool, error) {
	if md == nil && trigger == nil {
		return false, fmt.Errorf("unable to create key for syncer, none of metadata and trigger provided")
	}

	if trigger == nil {
		var err error
		trigger, err = createTriggerForSyncerConfigResourceFromMetadata(md)
		if err != nil {
			return false, fmt.Errorf("failed to create trigger for the syncer config: %w", err)
		}
	}

	exists := func() bool {
		c.syncerMu.RLock()
		defer c.syncerMu.RUnlock()

		if _, ok := c.syncerTriggerIndex[*trigger]; ok {
			// already created this syncer
			return true
		}
		return false
	}()

	if exists {
		return false, nil
	}

	logger := c.logger.WithName("syncer").
		WithFields(log.String("for", syncTarget.String()), log.String("config", trigger.String()))

	// create, start and register syncer
	var (
		config *syncer.Config
		err    error
	)
	switch trigger.kind {
	case configKindCM:
		logger.V("trying to get syncer config from configmap")
		var cm *corev1.ConfigMap
		cm, err = c.kubeClient.CoreV1().ConfigMaps(trigger.namespace).Get(c.ctx, trigger.name, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to get sync config configmap: %w", err)
		}
		config, err = getSyncerConfig(trigger.key, cm.Data, cm.BinaryData)
	case configKindSecret:
		logger.V("trying to get syncer config from secret")
		var secret *corev1.Secret
		secret, err = c.kubeClient.CoreV1().Secrets(trigger.namespace).Get(c.ctx, trigger.name, metav1.GetOptions{})
		if err != nil {
			return false, fmt.Errorf("failed to get sync config secret: %w", err)
		}
		config, err = getSyncerConfig(trigger.key, secret.StringData, secret.Data)
	}
	if err != nil {
		return false, fmt.Errorf("failed to get syncer config: %w", err)
	}

	s, err := syncer.NewSyncer(c.ctx, logger, config)
	if err != nil {
		return false, fmt.Errorf("failed to create syncer: %w", err)
	}

	err = s.Start(c.ctx.Done())
	if err != nil {
		_ = s.Stop()

		return false, fmt.Errorf("failed to start syncer: %w", err)
	}

	func() {
		c.syncerMu.Lock()
		defer c.syncerMu.Unlock()

		c.syncerTriggerIndex[*trigger] = &syncerSpec{
			targetConfig: syncTarget,
			syncerConfig: *trigger,
			syncer:       s,
		}
	}()

	go func(target configRef) {
		logger.I("starting config syncing routing")
		for update := range s.Retrieve() {
			var err error
			switch target.kind {
			case configKindCM:
				logger.V("got an update")
				err = c.updateConfigMapWithNewData(target.namespace, target.name, update)
			case configKindSecret:
				logger.V("got an update")
				err = c.updateSecretWithNewData(target.namespace, target.name, update)
			default:
				logger.V("unknown target kind")
				continue
			}

			logger.I("synced")
			if err != nil {
				logger.I("failed to update target config", log.Error(err))
			}
		}
	}(syncTarget)

	return true, nil
}

func (c *Controller) updateConfigMapWithNewData(namespace, name string, data map[string][]byte) error {
	key := namespace + "/" + name
	item, found, err := c.cmInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to find configmpa %q: %w", key, err)
	}
	if !found {
		return nil
	}

	cm, ok := item.(*corev1.ConfigMap)
	if !ok {
		return fmt.Errorf("invalid cache item, not a configmap: %t", item)
	}

	cm = cm.DeepCopy()
	if cm.Data == nil {
		cm.Data = make(map[string]string)
	}

	for k, d := range data {
		cm.Data[k] = string(d)
	}

	_, err = c.kubeClient.CoreV1().ConfigMaps(namespace).Update(c.ctx, cm, metav1.UpdateOptions{})
	if err != nil && !kubeerrors.IsNotFound(err) {
		return fmt.Errorf("failed to update configmap %q with new data: %w", key, err)
	}

	return nil
}

func (c *Controller) updateSecretWithNewData(namespace, name string, data map[string][]byte) error {
	key := namespace + "/" + name
	item, found, err := c.secretInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return fmt.Errorf("failed to find secret %q: %w", key, err)
	}
	if !found {
		return nil
	}

	secret, ok := item.(*corev1.Secret)
	if !ok {
		return fmt.Errorf("invalid cache item, not a secret: %t", item)
	}

	secret = secret.DeepCopy()
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}

	for k, d := range data {
		secret.Data[k] = d
	}

	_, err = c.kubeClient.CoreV1().Secrets(namespace).Update(c.ctx, secret, metav1.UpdateOptions{})
	if err != nil && !kubeerrors.IsNotFound(err) {
		return fmt.Errorf("failed to update secret %q with new data: %w", key, err)
	}

	return nil
}

func getSyncerConfig(key string, stringData map[string]string, binaryData map[string][]byte) (*syncer.Config, error) {
	d, ok := binaryData[key]
	if !ok {
		var strD string
		strD, ok = stringData[key]
		if !ok {
			return nil, fmt.Errorf("config not found for key %q", key)
		}

		d = []byte(strD)
	}

	config := new(syncer.Config)
	err := yaml.Unmarshal(d, config)
	if err != nil {
		err1 := json.Unmarshal(d, config)
		if err1 != nil {
			return nil, fmt.Errorf("failed to unmarshal syncer config: %w", multierr.Combine(err, err1))
		}
	}

	return config, nil
}
