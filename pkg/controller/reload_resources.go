package controller

import (
	"arhat.dev/pkg/log"
	"arhat.dev/pkg/reconcile"
	"arhat.dev/pkg/wellknownerrors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (c *Controller) OnPodUpdated(oldObj, newObj interface{}) *reconcile.Result {
	var (
		pod    = newObj.(*corev1.Pod).DeepCopy()
		logger = c.logger.WithFields(
			log.String("name", pod.Name),
			log.String("namespace", pod.Namespace),
			log.String("type", "reload:pod"),
		)
	)

	// pod added, check if the owner is managed by us
	var ownerRef *metav1.OwnerReference
	for i, o := range pod.OwnerReferences {
		if o.Controller != nil && *o.Controller {
			ownerRef = pod.OwnerReferences[i].DeepCopy()
		}
	}

	if ownerRef == nil {
		// poor pod, not able to be reloaded
		return nil
	}

	var (
		ownerKey = pod.Namespace + "/" + ownerRef.Name
		owner    metav1.ObjectMetaAccessor
		ok       bool
	)
	switch ownerRef.Kind {
	case "DaemonSet":
		dsObj, found, err := c.dsInformer.GetIndexer().GetByKey(ownerKey)
		if err != nil {
			logger.I("failed to get daemonset", log.String("ds", ownerKey), log.Error(err))
			return &reconcile.Result{Err: err}
		}

		if !found {
			logger.V("not managed by us")
			return nil
		}
		owner, ok = dsObj.(metav1.ObjectMetaAccessor)
	case "ReplicaSet":
		logger = logger.WithFields(log.String("ownerRef", "rs/"+ownerKey))
		rs, err := c.kubeClient.AppsV1().ReplicaSets(pod.Namespace).Get(c.ctx, ownerRef.Name, metav1.GetOptions{})
		if err != nil {
			logger.I("failed to get replicaset", log.String("rs", ownerKey), log.Error(err))
			return &reconcile.Result{Err: err}
		}

		for i, o := range rs.OwnerReferences {
			if o.Controller != nil && *o.Controller {
				ownerRef = rs.OwnerReferences[i].DeepCopy()
			}
		}
		ownerKey = pod.Namespace + "/" + ownerRef.Name

		logger = logger.WithFields(log.String("ownerRef", "dp/"+ownerKey))
		dpObj, found, err := c.deployInformer.GetIndexer().GetByKey(ownerKey)
		if err != nil {
			logger.I("failed to get deployment", log.String("dp", ownerKey), log.Error(err))
			return &reconcile.Result{Err: err}
		}

		if !found {
			logger.V("not managed by us")
			return nil
		}
		owner, ok = dpObj.(metav1.ObjectMetaAccessor)
	case "StatefulSet":
		logger = logger.WithFields(log.String("ownerRef", "sts/"+ownerKey))
		stsObj, found, err := c.stsInformer.GetIndexer().GetByKey(ownerKey)
		if err != nil {
			logger.I("failed to get statefulset", log.String("sts", ownerKey), log.Error(err))
			return &reconcile.Result{Err: err}
		}

		if !found {
			logger.V("not managed by us")
			return nil
		}
		owner, ok = stsObj.(metav1.ObjectMetaAccessor)
	default:
		logger.I("unknown pod controller", log.Any("controller", ownerRef))
		return nil
	}

	if !ok {
		logger.I("failed to convert pod controller object to meta accessor")
		return &reconcile.Result{Err: wellknownerrors.ErrInvalidOperation}
	}

	logger.D("creating pod specific triggers")
	// found the pod controller, get pod specific triggers for this pod
	triggers, err := c.createPodSpecificTriggers(logger, owner.GetObjectMeta(), pod)
	if err != nil {
		logger.I("failed to create pod specific triggers", log.Error(err))
		return &reconcile.Result{Err: err}
	}

	return c.ensureReloadObject(logger, createReloadKey(reloadKindPod, pod.Namespace, pod.Name), triggers)
}

func getReloadResourceMeta(obj interface{}) (kind reloadKind, namespace, name string) {
	switch o := obj.(type) {
	case *appsv1.DaemonSet:
		kind = reloadKindDaemonSet
		namespace, name = o.Namespace, o.Name
	case *appsv1.StatefulSet:
		kind = reloadKindStatefulSet
		namespace, name = o.Namespace, o.Name
	case *appsv1.Deployment:
		kind = reloadKindDeployment
		namespace, name = o.Namespace, o.Name
	}

	return
}

func getReloadResourceSpec(obj interface{}) (*metav1.ObjectMeta, *corev1.PodSpec) {
	switch o := obj.(type) {
	case *appsv1.DaemonSet:
		return o.ObjectMeta.DeepCopy(), o.Spec.Template.Spec.DeepCopy()
	case *appsv1.StatefulSet:
		return o.ObjectMeta.DeepCopy(), o.Spec.Template.Spec.DeepCopy()
	case *appsv1.Deployment:
		return o.ObjectMeta.DeepCopy(), o.Spec.Template.Spec.DeepCopy()
	}

	return nil, nil
}

func (c *Controller) OnReloadResourceUpdated(oldObj, newObj interface{}) *reconcile.Result {
	return c.ensureReloadObject(c.logger,
		createReloadKey(getReloadResourceMeta(newObj)),
		createReloadTriggers(getReloadResourceSpec(newObj)),
	)
}

func (c *Controller) OnReloadResourceDeleting(obj interface{}) *reconcile.Result {
	return c.ensureReloadObject(c.logger, createReloadKey(getReloadResourceMeta(obj)), nil)
}
