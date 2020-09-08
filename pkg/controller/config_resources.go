package controller

import (
	"arhat.dev/pkg/log"
	"arhat.dev/pkg/queue"
	"arhat.dev/pkg/reconcile"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"arhat.dev/ksync/pkg/constant"
)

func getTriggerMetaAndData(
	obj interface{},
) (
	kind configKind,
	namespace, name string,
	stringData map[string]string,
	binaryData map[string][]byte,
) {
	switch o := obj.(type) {
	case *corev1.ConfigMap:
		stringData, binaryData = o.Data, o.BinaryData
		kind, namespace, name = configKindCM, o.Namespace, o.Name
	case *corev1.Secret:
		kind, namespace, name = configKindCM, o.Namespace, o.Name
		stringData, binaryData = o.StringData, o.Data
	}

	return
}

func (c *Controller) OnConfigResourceAdded(obj interface{}) *reconcile.Result {
	kind, ns, name, stringData, binaryData := getTriggerMetaAndData(obj)
	logger := c.logger.WithFields(
		log.String("kind", string(kind)),
		log.String("namespace", ns),
		log.String("name", name),
	)

	logger.V("watching config")

	// TODO: no reload will be triggered at this time since they are just recognized by this controller,
	// 		 no old hash record exists
	// 		 we should consider add annotation to these config maps
	c.updateTriggerSourceHashes(buildTriggerSourceHash(kind, ns, name, stringData, binaryData))

	if o, ok := obj.(metav1.ObjectMetaAccessor); ok && isConfigRequireSynced(o) {
		return &reconcile.Result{NextAction: queue.ActionUpdate}
	}

	return nil
}

func (c *Controller) OnConfigResourceUpdated(oldObj, newObj interface{}) *reconcile.Result {
	kind, ns, name, stringData, binaryData := getTriggerMetaAndData(newObj)
	logger := c.logger.WithFields(
		log.String("kind", string(kind)),
		log.String("namespace", ns),
		log.String("name", name),
	)

	logger.V("updated config")

	c.notifyUpdate(logger, buildTriggerSourceHash(kind, ns, name, stringData, binaryData))

	var (
		created bool
		err     error
	)

	newMeta, ok := newObj.(metav1.ObjectMetaAccessor)
	if ok && isConfigRequireSynced(newMeta) {
		// this config needs to be synced with remote source
		logger.D("ensuring syncer for this config")
		created, err = c.ensureSyncer(createConfigRef(kind, ns, name, ""), newMeta, nil)
		if err != nil {
			logger.I(err.Error())
			return &reconcile.Result{Err: err}
		}

		logger.I("syncer ensured for this config")
	}

	if created {
		if old, ok := oldObj.(metav1.ObjectMetaAccessor); ok && isConfigRequireSynced(old) {
			oldAnno := old.GetObjectMeta().GetAnnotations()
			if len(oldAnno) == 0 {
				return nil
			}

			newAnno := newMeta.GetObjectMeta().GetAnnotations()

			if newAnno[constant.AnnotationSyncConfig] != oldAnno[constant.AnnotationSyncConfig] {
				// syncer config ref changed, need to remove old syncer
				logger.D("removing old syncer due to config ref changed")
				err := c.removeSyncer(old, nil)
				if err != nil {
					logger.I("failed to remove syncer", log.Error(err))
					return &reconcile.Result{Err: err}
				}
			}
		}
	}

	return nil
}

func (c *Controller) OnConfigResourceDeleting(obj interface{}) *reconcile.Result {
	kind, ns, name, stringData, binaryData := getTriggerMetaAndData(obj)
	logger := c.logger.WithFields(
		log.String("type",
			string(kind)),
		log.String("namespace", ns),
		log.String("name", name),
	)

	logger.V("removed by others")

	c.removeTriggerSourceHashes(buildTriggerSourceHash(kind, ns, name, stringData, binaryData))

	if o, ok := obj.(metav1.ObjectMetaAccessor); ok && isConfigRequireSynced(o) {
		logger.D("removing config syncer if any")
		err := c.removeSyncer(o, nil)
		if err != nil {
			logger.I("failed to remove syncer", log.Error(err))
			return &reconcile.Result{Err: err}
		}
		logger.V("config syncer removed")

		return nil
	}

	return nil
}
