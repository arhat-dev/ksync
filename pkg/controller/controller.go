package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"arhat.dev/pkg/backoff"
	"arhat.dev/pkg/envhelper"
	"arhat.dev/pkg/kubehelper"
	"arhat.dev/pkg/log"
	"arhat.dev/pkg/queue"
	"arhat.dev/pkg/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/informers"
	informersappsv1 "k8s.io/client-go/informers/apps/v1"
	informerscorev1 "k8s.io/client-go/informers/core/v1"
	kubeclient "k8s.io/client-go/kubernetes"
	kubecache "k8s.io/client-go/tools/cache"

	"arhat.dev/ksync/pkg/conf"
	"arhat.dev/ksync/pkg/constant"
)

func NewController(appCtx context.Context, config *conf.KsyncConfig) (*Controller, error) {
	var (
		kubeClient, _, err = config.Ksync.KubeClient.NewKubeClient(nil, true)
		namespace          = corev1.NamespaceAll
		fieldSelector      = fields.Everything().String()
		ctrlCtx, exitCtrl  = context.WithCancel(appCtx)
	)
	_ = exitCtrl

	if err != nil {
		return nil, fmt.Errorf("failed to create kube client for controller: %w", err)
	}

	if config.Ksync.Namespaced {
		namespace = envhelper.ThisPodNS()
	} else {
		var (
			enabledNamespaces  []string
			disabledNamespaces = config.Ksync.IgnoredNamespaces
		)
		err = func() error {
			nsProbeCtx, cancelProbe := context.WithTimeout(appCtx, 10*time.Second)
			defer cancelProbe()

			disableReq, err2 := labels.NewRequirement(constant.LabelDisabled, selection.Exists, nil)
			if err2 != nil {
				return fmt.Errorf("failed to create ns disabled requirement: %w", err2)
			}

			// find disabled namespaces
			nsList, err2 := kubeClient.CoreV1().Namespaces().List(nsProbeCtx, metav1.ListOptions{
				LabelSelector: labels.NewSelector().Add(*disableReq).String(),
			})
			if err2 != nil {
				if errors.IsForbidden(err2) {
					return nil
				}
				return fmt.Errorf("failed to get disabled namespacs: %w", err2)
			}
			for _, ns := range nsList.Items {
				disabledNamespaces = append(disabledNamespaces, ns.Name)
			}

			enableReq, err2 := labels.NewRequirement(constant.LabelEnabled, selection.Exists, nil)
			if err2 != nil {
				return fmt.Errorf("failed to create ns enabled requirement: %w", err2)
			}

			// find disabled namespaces
			nsList, err2 = kubeClient.CoreV1().Namespaces().List(nsProbeCtx, metav1.ListOptions{
				LabelSelector: labels.NewSelector().Add(*enableReq).String(),
			})
			if err2 != nil {
				// we have checked permission before
				return fmt.Errorf("failed to get enabled namespacs: %w", err2)
			}
			for _, ns := range nsList.Items {
				enabledNamespaces = append(enabledNamespaces, ns.Name)
			}

			return nil
		}()
		if err != nil {
			return nil, fmt.Errorf("failed to determine namespaces to watch: %w", err)
		}

		// only use enabled namespaces if specified
		if len(enabledNamespaces) > 0 {
			// TODO: kubernetes field selector do not support logic 'or', we should implement ours
		} else {
			var selectors []fields.Selector
			for _, ns := range disabledNamespaces {
				selectors = append(selectors, fields.OneTermNotEqualSelector("metadata.namespace", ns))
			}
			fieldSelector = fields.AndSelectors(selectors...).String()
		}
	}

	informerFactory := informers.NewSharedInformerFactory(kubeClient, 0)

	configResourceInformerFactory := informerscorev1.New(informerFactory, namespace, func(options *metav1.ListOptions) {
		options.FieldSelector = fieldSelector
	})

	cmInformer := configResourceInformerFactory.ConfigMaps().Informer()
	secretInformer := configResourceInformerFactory.Secrets().Informer()

	reloadResourceInformerFactory := informersappsv1.New(informerFactory, namespace, func(options *metav1.ListOptions) {
		options.LabelSelector = labels.FormatLabels(map[string]string{
			constant.LabelAction: constant.LabelActionValueReload,
		})
		options.FieldSelector = fieldSelector
	})

	deployInformer := reloadResourceInformerFactory.Deployments().Informer()
	dsInformer := reloadResourceInformerFactory.DaemonSets().Informer()
	stsInformer := reloadResourceInformerFactory.StatefulSets().Informer()

	podInformerFactory := informerscorev1.New(informerFactory, namespace, func(options *metav1.ListOptions) {
		// TODO: support custom labels
		options.FieldSelector = fieldSelector
	})
	podInformer := podInformerFactory.Pods().Informer()

	ctrl := &Controller{
		ctx:  ctrlCtx,
		exit: exitCtrl,

		kubeClient: kubeClient,

		logger: log.Log.WithName("controller"),

		informerFactory: informerFactory,

		informersSyncWait: []kubecache.InformerSynced{
			cmInformer.HasSynced,
			secretInformer.HasSynced,

			deployInformer.HasSynced,
			dsInformer.HasSynced,
			stsInformer.HasSynced,
			podInformer.HasSynced,
		},

		cmInformer:     cmInformer,
		secretInformer: secretInformer,

		dsInformer:     dsInformer,
		deployInformer: deployInformer,
		stsInformer:    stsInformer,
		podInformer:    podInformer,

		reloadDelay: config.Ksync.ReloadDelay,

		reloadTriggerIndex:      make(map[configRef]map[reloadObjectKey]struct{}),
		reloadTriggerSourceHash: make(map[configRef]string),
		mu:                      new(sync.RWMutex),

		syncerTriggerIndex: make(map[configRef]*syncerSpec),
		syncerMu:           new(sync.RWMutex),
	}

	ctrl.listActions = []func() error{
		// config resources
		func() error {
			_, err := configResourceInformerFactory.ConfigMaps().Lister().List(labels.Everything())
			return err
		},
		func() error {
			_, err := configResourceInformerFactory.Secrets().Lister().List(labels.Everything())
			return err
		},
		// reload resources
		func() error {
			_, err := reloadResourceInformerFactory.Deployments().Lister().List(labels.Everything())
			return err
		},
		func() error {
			_, err := reloadResourceInformerFactory.DaemonSets().Lister().List(labels.Everything())
			return err
		},
		func() error {
			_, err := reloadResourceInformerFactory.StatefulSets().Lister().List(labels.Everything())
			return err
		},
		func() error {
			_, err := podInformerFactory.Pods().Lister().List(labels.Everything())
			return err
		},
	}

	nextUpdate := func(obj interface{}) *reconcile.Result {
		return &reconcile.Result{NextAction: queue.ActionUpdate}
	}

	ctrl.cmRec = kubehelper.NewKubeInformerReconciler(ctrlCtx, cmInformer, reconcile.Options{
		Logger:       log.Log.WithName("conf:cm"),
		RequireCache: true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    ctrl.OnConfigResourceAdded,
			OnUpdated:  ctrl.OnConfigResourceUpdated,
			OnDeleting: ctrl.OnConfigResourceDeleting,
			OnDeleted:  ctrl.OnConfigResourceDeleting,
		},
	})

	ctrl.secretRec = kubehelper.NewKubeInformerReconciler(ctrlCtx, secretInformer, reconcile.Options{
		Logger:       log.Log.WithName("conf:secrets"),
		RequireCache: true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    ctrl.OnConfigResourceAdded,
			OnUpdated:  ctrl.OnConfigResourceUpdated,
			OnDeleting: ctrl.OnConfigResourceDeleting,
			OnDeleted:  ctrl.OnConfigResourceDeleting,
		},
	})

	ctrl.deployRec = kubehelper.NewKubeInformerReconciler(ctrlCtx, deployInformer, reconcile.Options{
		Logger:       log.Log.WithName("reload:deploy"),
		RequireCache: true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    nextUpdate,
			OnUpdated:  ctrl.OnReloadResourceUpdated,
			OnDeleting: ctrl.OnReloadResourceDeleting,
			OnDeleted:  ctrl.OnReloadResourceDeleting,
		},
	})

	ctrl.dsRec = kubehelper.NewKubeInformerReconciler(ctrlCtx, dsInformer, reconcile.Options{
		Logger:       log.Log.WithName("reload:ds"),
		RequireCache: true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    nextUpdate,
			OnUpdated:  ctrl.OnReloadResourceUpdated,
			OnDeleting: ctrl.OnReloadResourceDeleting,
			OnDeleted:  ctrl.OnReloadResourceDeleting,
		},
	})

	ctrl.stsRec = kubehelper.NewKubeInformerReconciler(ctrlCtx, stsInformer, reconcile.Options{
		Logger:       log.Log.WithName("reload:sts"),
		RequireCache: true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    nextUpdate,
			OnUpdated:  ctrl.OnReloadResourceUpdated,
			OnDeleting: ctrl.OnReloadResourceDeleting,
			OnDeleted:  ctrl.OnReloadResourceDeleting,
		},
	})

	ctrl.podRec = kubehelper.NewKubeInformerReconciler(ctrlCtx, podInformer, reconcile.Options{
		Logger:       log.Log.WithName("reload:pod"),
		RequireCache: true,
		Handlers: reconcile.HandleFuncs{
			OnAdded:    nextUpdate,
			OnUpdated:  ctrl.OnPodUpdated,
			OnDeleting: nextUpdate,
			OnDeleted:  nextUpdate,
		},
	})

	ctrl.reloadRec = reconcile.NewCore(ctrlCtx, &reconcile.Options{
		Logger:          log.Log.WithName("sched:reload"),
		RequireCache:    true,
		BackoffStrategy: backoff.NewStrategy(time.Second, time.Minute, 2, 0),
		Handlers: reconcile.HandleFuncs{
			OnAdded: ctrl.handleWorkloadReload,
		},
	})

	ctrl.syncRec = reconcile.NewCore(ctrlCtx, &reconcile.Options{
		Logger:          log.Log.WithName("sched:sync"),
		RequireCache:    true,
		BackoffStrategy: backoff.NewStrategy(time.Second, time.Minute, 2, 0),
		Handlers: reconcile.HandleFuncs{
			OnAdded: ctrl.handleSyncerConfigUpdate,
		},
	})

	ctrl.reconcilesStart = []func() error{
		ctrl.cmRec.Start,
		ctrl.secretRec.Start,

		ctrl.deployRec.Start,
		ctrl.dsRec.Start,
		ctrl.stsRec.Start,
		ctrl.podRec.Start,

		ctrl.reloadRec.Start,
		ctrl.syncRec.Start,
	}

	ctrl.reconcileUntil = []func(<-chan struct{}){
		ctrl.cmRec.ReconcileUntil,
		ctrl.secretRec.ReconcileUntil,

		ctrl.deployRec.ReconcileUntil,
		ctrl.dsRec.ReconcileUntil,
		ctrl.stsRec.ReconcileUntil,
		ctrl.podRec.ReconcileUntil,

		ctrl.reloadRec.ReconcileUntil,
		ctrl.syncRec.ReconcileUntil,
	}

	return ctrl, nil
}

type Controller struct {
	ctx  context.Context
	exit context.CancelFunc

	kubeClient kubeclient.Interface

	logger          log.Interface
	informerFactory informers.SharedInformerFactory

	informersSyncWait []kubecache.InformerSynced
	listActions       []func() error
	reconcilesStart   []func() error
	reconcileUntil    []func(<-chan struct{})

	cmRec     *kubehelper.KubeInformerReconciler
	secretRec *kubehelper.KubeInformerReconciler

	deployRec *kubehelper.KubeInformerReconciler
	dsRec     *kubehelper.KubeInformerReconciler
	stsRec    *kubehelper.KubeInformerReconciler
	podRec    *kubehelper.KubeInformerReconciler

	dsInformer     kubecache.SharedIndexInformer
	deployInformer kubecache.SharedIndexInformer
	stsInformer    kubecache.SharedIndexInformer
	podInformer    kubecache.SharedIndexInformer

	cmInformer     kubecache.SharedIndexInformer
	secretInformer kubecache.SharedIndexInformer

	reloadDelay time.Duration
	reloadRec   *reconcile.Core
	syncRec     *reconcile.Core

	// reload related
	reloadTriggerIndex      map[configRef]map[reloadObjectKey]struct{}
	reloadTriggerSourceHash map[configRef]string
	mu                      *sync.RWMutex

	syncerTriggerIndex map[configRef]*syncerSpec
	syncerMu           *sync.RWMutex
}

func (c *Controller) Start() error {
	c.informerFactory.Start(c.ctx.Done())

	for _, startReconcile := range c.reconcilesStart {
		if err := startReconcile(); err != nil {
			return fmt.Errorf("failed to start reconciler: %w", err)
		}
	}

	for _, doList := range c.listActions {
		if err := doList(); err != nil {
			return fmt.Errorf("failed to do list action for cache sync: %w", err)
		}
	}

	if !kubecache.WaitForCacheSync(c.ctx.Done(), c.informersSyncWait...) {
		return fmt.Errorf("informer cache not synced")
	}

	for _, reconcileUntil := range c.reconcileUntil {
		go reconcileUntil(c.ctx.Done())
	}

	<-c.ctx.Done()

	return nil
}
