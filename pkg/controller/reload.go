package controller

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"arhat.dev/pkg/hashhelper"
	"arhat.dev/pkg/log"
	"arhat.dev/pkg/patchhelper"
	"arhat.dev/pkg/reconcile"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/third_party/forked/golang/expansion"

	"arhat.dev/ksync/pkg/constant"
)

func (c *Controller) handleWorkloadReload(obj interface{}) *reconcile.Result {
	logger := c.logger.WithFields(log.String("action", "reload"))
	spec, ok := obj.(*reloadSpec)
	if !ok {
		logger.I("invalid reload job key, not a reloadSpec", log.String("keyType", reflect.TypeOf(obj).String()))
		return nil
	}

	targetKey := spec.namespace + "/" + spec.name
	logger = logger.WithFields(log.String("target", targetKey))

	var (
		podTemplate *corev1.PodTemplateSpec

		// to generalize patch action
		target                      interface{}
		kubeResourceKind            interface{}
		doPatch                     func(data []byte) error
		getObjectWithNewPodTemplate func(*corev1.PodTemplateSpec) interface{}
	)
	switch spec.kind {
	case reloadKindPod:
		logger = logger.WithFields(log.String("kind", "pod"))

		obj, ok, err := c.podInformer.GetIndexer().GetByKey(targetKey)
		if err != nil {
			logger.I("failed to get pod from informer cache by key", log.Error(err))
			return &reconcile.Result{Err: err}
		}
		if !ok {
			logger.I("cache not found")
			return nil
		}

		pod, ok := obj.(*corev1.Pod)
		if !ok {
			logger.I("cache not valid")
			return nil
		}

		err = c.kubeClient.CoreV1().Pods(spec.namespace).
			Delete(c.ctx, spec.name, metav1.DeleteOptions{GracePeriodSeconds: pod.Spec.TerminationGracePeriodSeconds})
		if err != nil && !kubeerrors.IsNotFound(err) {
			logger.I("failed to kill pod", log.Error(err))
			return &reconcile.Result{Err: err}
		}

		return nil
	case reloadKindDaemonSet:
		logger = logger.WithFields(log.String("kind", "ds"))

		obj, ok, err := c.dsInformer.GetIndexer().GetByKey(targetKey)
		if err != nil {
			logger.I("failed to get daemonset from informer cache", log.Error(err))
			return &reconcile.Result{Err: err}
		}
		if !ok {
			logger.I("cache not found")
			return nil
		}

		ds, ok := obj.(*appsv1.DaemonSet)
		if !ok {
			logger.I("cache not valid")
			return nil
		}

		target = ds
		kubeResourceKind = new(appsv1.DaemonSet)
		getObjectWithNewPodTemplate = func(spec *corev1.PodTemplateSpec) interface{} {
			s := ds.DeepCopy()
			s.Spec.Template = *spec
			return s
		}
		doPatch = func(data []byte) error {
			_, err := c.kubeClient.AppsV1().DaemonSets(ds.Namespace).
				Patch(c.ctx, ds.Name, types.StrategicMergePatchType, data, metav1.PatchOptions{})
			return err
		}
		podTemplate = ds.Spec.Template.DeepCopy()
	case reloadKindDeployment:
		logger = logger.WithFields(log.String("kind", "deploy"))

		obj, ok, err := c.deployInformer.GetIndexer().GetByKey(targetKey)
		if err != nil {
			logger.I("failed to get deployment from informer cache", log.Error(err))
			return &reconcile.Result{Err: err}
		}
		if !ok {
			logger.I("cache not found")
			return nil
		}

		deploy, ok := obj.(*appsv1.Deployment)
		if !ok {
			logger.I("cache not valid")
			return nil
		}

		target = deploy
		kubeResourceKind = new(appsv1.Deployment)
		getObjectWithNewPodTemplate = func(spec *corev1.PodTemplateSpec) interface{} {
			s := deploy.DeepCopy()
			s.Spec.Template = *spec
			return s
		}
		doPatch = func(data []byte) error {
			_, err := c.kubeClient.AppsV1().Deployments(deploy.Namespace).
				Patch(c.ctx, deploy.Name, types.StrategicMergePatchType, data, metav1.PatchOptions{})
			return err
		}
		podTemplate = deploy.Spec.Template.DeepCopy()
	case reloadKindStatefulSet:
		logger = logger.WithFields(log.String("kind", "sts"))

		obj, ok, err := c.stsInformer.GetIndexer().GetByKey(targetKey)
		if err != nil {
			logger.I("failed to get statefulset from informer cache", log.Error(err))
			return &reconcile.Result{Err: err}
		}
		if !ok {
			logger.I("cache not found")
			return nil
		}

		sts, ok := obj.(*appsv1.StatefulSet)
		if !ok {
			logger.I("cache not valid")
			return nil
		}

		target = sts
		kubeResourceKind = new(appsv1.StatefulSet)
		getObjectWithNewPodTemplate = func(spec *corev1.PodTemplateSpec) interface{} {
			s := sts.DeepCopy()
			s.Spec.Template = *spec
			return s
		}
		doPatch = func(data []byte) error {
			_, err := c.kubeClient.AppsV1().StatefulSets(sts.Namespace).
				Patch(c.ctx, sts.Name, types.StrategicMergePatchType, data, metav1.PatchOptions{})
			return err
		}
		podTemplate = sts.Spec.Template.DeepCopy()
	default:
		logger.I("unknown kind", log.String("kind", string(spec.kind)))
		return nil
	}

	// generate config hash from all triggers related
	hashes := make(map[string]string)
	func() {
		c.mu.RLock()
		defer c.mu.RUnlock()

		for t := range spec.triggers {
			hash, ok := c.reloadTriggerSourceHash[createConfigRef(t.kind, t.namespace, t.name, t.key)]
			if !ok {
				continue
			}

			hashKey := constant.AnnotationHashPrefix + "/" + hashhelper.MD5SumHex([]byte(
				filepath.Clean(filepath.Join(string(t.kind), t.namespace, t.name, t.key)),
			))
			hashes[hashKey] = fmt.Sprintf("sha256:%s", hash)
		}
	}()

	if podTemplate.Annotations == nil {
		podTemplate.Annotations = make(map[string]string)
	}

	for k, v := range hashes {
		podTemplate.Annotations[k] = v
	}

	logger.I("patching to rollout new config")
	err := patchhelper.TwoWayMergePatch(target, getObjectWithNewPodTemplate(podTemplate), kubeResourceKind, doPatch)
	if err != nil {
		logger.I("failed to patch update", log.Error(err))
		if kubeerrors.IsNotFound(err) {
			return nil
		}

		return &reconcile.Result{Err: err}
	}

	return nil
}

func (c *Controller) ensureReloadObject(
	logger log.Interface,
	key reloadObjectKey,
	triggers map[configRef]struct{},
) *reconcile.Result {
	logger = logger.WithFields(
		log.String("name", key.name),
		log.String("namespace", key.namespace),
		log.String("type", string(key.kind)),
	)

	if len(triggers) == 0 {
		// disable
		logger.V("ignored due to no reload triggers")
		return nil
	}

	if logger.Enabled(log.LevelVerbose) {
		var triggerList []string
		for t := range triggers {
			triggerList = append(triggerList, t.String())
		}

		logger.V(fmt.Sprintf("%s will be triggered", key.String()), log.Strings("by", triggerList))
	}

	c.mu.Lock()
	defer func() {
		c.mu.Unlock()

		func() {
			c.mu.RLock()
			defer c.mu.RUnlock()

			if !logger.Enabled(log.LevelVerbose) {
				return
			}

			logger.V("inspecting existing triggers")
			for t, r := range c.reloadTriggerIndex {
				var reloadKeys []string
				for rl := range r {
					reloadKeys = append(reloadKeys, rl.String())
				}

				logger.V(fmt.Sprintf("%s will trigger reload", t.String()), log.Strings("for", reloadKeys))
			}
		}()
	}()

	// mark old triggers
	triggersToRemove := make(map[configRef]struct{})
	for t, rs := range c.reloadTriggerIndex {
		if len(rs) != 0 {
			// remove triggers related to this key
			delete(rs, key)
		}

		if len(rs) == 0 {
			logger.V("marked trigger to be removed", log.String("key", t.String()))
			triggersToRemove[t] = struct{}{}
		}
	}

	// add new triggers
	for t := range triggers {
		// this trigger is required, do not remove
		delete(triggersToRemove, t)

		if m, ok := c.reloadTriggerIndex[t]; !ok || m == nil {
			c.reloadTriggerIndex[t] = make(map[reloadObjectKey]struct{})
		}

		c.reloadTriggerIndex[t][key] = struct{}{}
	}

	// cleanup trigger index
	for t := range triggersToRemove {
		logger.V("removing unused trigger", log.String("key", t.String()))
		delete(c.reloadTriggerIndex, t)
	}

	return nil
}

// nolint:gocyclo
func createReloadTriggers(md *metav1.ObjectMeta, spec *corev1.PodSpec) map[configRef]struct{} {
	var (
		// map[name] -> set[key]
		userRestrictedCMs     = false
		userRestrictedSecrets = false
		disablePodCMs         = false
		disablePodSecrets     = false
		podCMs                = make(map[string]map[string]struct{})
		podSecrets            = make(map[string]map[string]struct{})
		forceCMs              = make(map[string]map[string]struct{})
		forceSecrets          = make(map[string]map[string]struct{})
	)

	if len(md.Annotations) != 0 {
		for _, spec := range []struct {
			annoKey    string
			m          map[string]map[string]struct{}
			restricted *bool
			disabled   *bool
		}{
			{
				annoKey:    constant.AnnotationConfigMaps,
				m:          podCMs,
				restricted: &userRestrictedCMs,
				disabled:   &disablePodCMs},
			{
				annoKey:    constant.AnnotationSecrets,
				m:          podSecrets,
				restricted: &userRestrictedSecrets,
				disabled:   &disablePodSecrets,
			},
			{
				annoKey: constant.AnnotationForceConfigMaps,
				m:       forceCMs,
			},
			{
				annoKey: constant.AnnotationForceSecrets,
				m:       forceSecrets,
			},
		} {
			names, ok := md.Annotations[spec.annoKey]
			if !ok {
				// no annotation -> no restriction
				if spec.restricted != nil {
					*spec.restricted = false
				}
				continue
			}

			if spec.restricted != nil {
				*spec.restricted = true
			}

			if names == "" {
				// annotation with no value -> disable this kind of trigger
				if spec.disabled != nil {
					*spec.disabled = true
				}

				continue
			}

			for _, p := range getNameAndOptionalKey(names) {
				if strings.Contains(p.name, "$") || strings.Contains(p.key, "$") {
					// pod specific values, do not eval now
					continue
				}

				if p.key == "" {
					// no keys, use default ones or as a whole
					spec.m[p.name] = nil
				} else {
					if d, ok := spec.m[p.name]; !ok || d == nil {
						spec.m[p.name] = make(map[string]struct{})
					}

					spec.m[p.name][p.key] = struct{}{}
				}
			}
		}
	}

	if disablePodCMs && disablePodSecrets &&
		len(forceCMs) == 0 && len(forceSecrets) == 0 {
		// no triggers
		return nil
	}

	triggers := make(map[configRef]struct{})
	for _, vol := range spec.Volumes {
		switch {
		case vol.ConfigMap != nil:
			if disablePodCMs {
				// pod cm trigger disabled
				continue
			}

			cm := vol.ConfigMap
			if userRestrictedCMs {
				keys, ok := podCMs[cm.Name]
				if !ok {
					// this cm volume is not intended for reload trigger
					continue
				}

				if keys != nil {
					// user specified keys, do not use default ones
					continue
				}
			}

			// find subPath mount first
			requireWhole := false
			added := false
		loopContainersForConfigMaps:
			for _, ctr := range spec.Containers {
				for _, mo := range ctr.VolumeMounts {
					if mo.SubPath == "" && mo.SubPathExpr == "" {
						requireWhole = true
						break loopContainersForConfigMaps
					}

					if mo.SubPath != "" {
						added = true
						triggers[createConfigRef(configKindCM, md.Namespace, cm.Name, mo.SubPath)] = struct{}{}
					}
				}
			}

			// use whole cm volume, check actual actual keys used
			if requireWhole {
				if len(cm.Items) != 0 {
					for _, item := range cm.Items {
						added = true
						triggers[createConfigRef(configKindCM, md.Namespace, cm.Name, item.Key)] = struct{}{}
					}
				} else {
					added = true
					triggers[createConfigRef(configKindCM, md.Namespace, cm.Name, "")] = struct{}{}
				}
			}

			// user specified cm name, but not used in pod spec, can be loaded from init container
			// for some reason, add it as a non-controllerLevel trigger
			if !added {
				triggers[createConfigRef(configKindCM, md.Namespace, cm.Name, "")] = struct{}{}
			}

			delete(podCMs, cm.Name)
		case vol.Secret != nil:
			if disablePodSecrets {
				// pod secret trigger disabled
				continue
			}

			secret := vol.Secret
			if userRestrictedSecrets {
				keys, ok := podSecrets[secret.SecretName]
				if !ok {
					// this secret is not intended for reload trigger
					continue
				}

				if keys != nil {
					// user specified keys, do not use default ones
					continue
				}
			}

			// find subPath mount first
			added := false
			requireWhole := false
		loopContainersForSecrets:
			for _, ctr := range spec.Containers {
				for _, mo := range ctr.VolumeMounts {
					if mo.SubPath == "" && mo.SubPathExpr == "" {
						requireWhole = true
						break loopContainersForSecrets
					}

					if mo.SubPath != "" {
						added = true
						triggers[createConfigRef(
							configKindSecret,
							md.Namespace,
							secret.SecretName,
							mo.SubPath)] = struct{}{}
					}
				}
			}

			// use whole secret, check used keys
			if requireWhole {
				if len(secret.Items) != 0 {
					for _, item := range secret.Items {
						added = true
						triggers[createConfigRef(
							configKindSecret,
							md.Namespace,
							secret.SecretName,
							item.Key)] = struct{}{}
					}
				} else {
					added = true
					triggers[createConfigRef(configKindSecret, md.Namespace, secret.SecretName, "")] = struct{}{}
				}
			}

			if !added {
				triggers[createConfigRef(configKindSecret, md.Namespace, secret.SecretName, "")] = struct{}{}
			}

			delete(podSecrets, secret.SecretName)
		}
	}

	for _, spec := range []struct {
		kind configKind
		m    map[string]map[string]struct{}
	}{
		{kind: configKindCM, m: podCMs},
		{kind: configKindCM, m: forceCMs},
		{kind: configKindSecret, m: podSecrets},
		{kind: configKindSecret, m: forceSecrets},
	} {
		for name, keys := range spec.m {
			if len(keys) == 0 {
				triggers[createConfigRef(spec.kind, md.Namespace, name, "")] = struct{}{}
				continue
			}

			for k := range keys {
				triggers[createConfigRef(spec.kind, md.Namespace, name, k)] = struct{}{}
			}
		}
	}

	return triggers
}

// nolint:gocyclo
func (c *Controller) createPodSpecificTriggers(
	logger log.Interface,
	podOwnerMetadata metav1.Object,
	pod *corev1.Pod,
) (map[configRef]struct{}, error) {
	var (
		cmVols     = make(map[string]*corev1.ConfigMapVolumeSource)
		secretVols = make(map[string]*corev1.SecretVolumeSource)
		triggers   = make(map[configRef]struct{})
	)

	getTriggerKind := func(name string) configKind {
		_, ok := cmVols[name]
		if ok {
			return configKindCM
		}

		_, ok = secretVols[name]
		if ok {
			return configKindSecret
		}

		return ""
	}

	getTriggerName := func(name string) string {
		v, ok := cmVols[name]
		if ok {
			return v.Name
		}

		vo, ok := secretVols[name]
		if ok {
			return vo.SecretName
		}

		return ""
	}

	for _, vol := range pod.Spec.Volumes {
		switch {
		case vol.ConfigMap != nil:
			cmVols[vol.Name] = vol.ConfigMap
		case vol.Secret != nil:
			secretVols[vol.Name] = vol.Secret
		}
	}

	evalAnnotation := false
loopAnnotations:
	for k, v := range podOwnerMetadata.GetAnnotations() {
		switch k {
		case constant.AnnotationSecrets,
			constant.AnnotationForceSecrets,
			constant.AnnotationConfigMaps,
			constant.AnnotationForceConfigMaps:
			if !strings.Contains(v, "$") {
				continue
			}
			logger.V("found pod specific annotation trigger(s)")
			evalAnnotation = true
			break loopAnnotations
		}
	}

	var allCtrEnvs []kubecontainer.EnvVar

	if evalAnnotation {
		logger.V("resolving init container envs")
		// loop all init containers just to get env vars
		for i := range pod.Spec.InitContainers {
			envs, err := c.expandContainersEnvs(pod, &pod.Spec.InitContainers[i])
			if err != nil {
				return nil, fmt.Errorf("failed to expand container envs: %w", err)
			}
			allCtrEnvs = append(allCtrEnvs, envs...)
		}
	}

	evalPod := len(cmVols) != 0 || len(secretVols) != 0
	if evalAnnotation || evalPod {
		logger.V("resolving work container envs")
		for i, ctr := range pod.Spec.Containers {
			envs, err := c.expandContainersEnvs(pod, &pod.Spec.Containers[i])
			if err != nil {
				return nil, fmt.Errorf("failed to expand container envs: %w", err)
			}
			allCtrEnvs = append(allCtrEnvs, envs...)

			if !evalPod {
				continue
			}

			envMap := envVarsToMap(envs)
			for _, mo := range ctr.VolumeMounts {
				kind := getTriggerKind(mo.Name)
				if kind == "" {
					// is not configmap or secret
					continue
				}

				if !strings.Contains(mo.SubPathExpr, "$") {
					continue
				}

				failed := false
				key := expansion.Expand(mo.SubPathExpr, func(key string) string {
					value, ok := envMap[key]
					if !ok || len(value) == 0 {
						failed = true
					}
					return value
				})
				if failed {
					return nil, fmt.Errorf("failed to expand subPathExpr for container %q", ctr.Name)
				}

				name := getTriggerName(mo.Name)
				if name == "" {
					// NOTE: this should not happen since we have survived the trigger kind check
					return nil, fmt.Errorf("failed to get source name for trigger")
				}
				triggers[createConfigRef(kind, pod.Namespace, name, key)] = struct{}{}
			}
		}
	}

	if evalAnnotation {
		logger.V("resolving annotated triggers")
		envMap := envVarsToMap(allCtrEnvs)
		for k, v := range podOwnerMetadata.GetAnnotations() {
			var kind configKind
			switch k {
			case constant.AnnotationSecrets,
				constant.AnnotationForceSecrets:
				if !strings.Contains(v, "$") {
					continue
				}
				kind = configKindSecret
			case constant.AnnotationConfigMaps,
				constant.AnnotationForceConfigMaps:
				if !strings.Contains(v, "$") {
					continue
				}
				kind = configKindCM
			default:
				continue
			}

			// annotation value contains `$`, need to eval
			for _, p := range getNameAndOptionalKey(v) {
				nameIsExpr := strings.Contains(p.name, "$")
				keyIsExpr := strings.Contains(p.key, "$")

				if !nameIsExpr && !keyIsExpr {
					continue
				}

				var (
					name, key string
					failed    bool
				)

				name = p.name
				key = p.key

				if nameIsExpr {
					name = expansion.Expand(p.name, func(key string) string {
						value, ok := envMap[key]
						if !ok || len(value) == 0 {
							failed = true
						}
						return value
					})

					if failed {
						return nil, fmt.Errorf("failed to expand name expr %q", p.name)
					}
				}

				if keyIsExpr {
					key = expansion.Expand(p.key, func(key string) string {
						value, ok := envMap[key]
						if !ok || len(value) == 0 {
							failed = true
						}
						return value
					})

					if failed {
						return nil, fmt.Errorf("failed to expand key expr %q", p.key)
					}
				}

				triggers[createConfigRef(kind, pod.Namespace, name, key)] = struct{}{}
			}
		}
	}

	return triggers, nil
}

// envVarsToMap constructs a map of environment name to value from a slice
// of env vars.
func envVarsToMap(envs []kubecontainer.EnvVar) map[string]string {
	result := map[string]string{}
	for _, env := range envs {
		result[env.Name] = env.Value
	}
	return result
}
