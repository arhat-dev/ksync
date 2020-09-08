package controller

import "arhat.dev/ksync/pkg/syncer"

// kinds of objects to be reloaded
type reloadKind string

const (
	reloadKindDaemonSet   reloadKind = "reload:ds"
	reloadKindDeployment  reloadKind = "reload:deploy"
	reloadKindStatefulSet reloadKind = "reload:sts"
	reloadKindPod         reloadKind = "reload:pod"
)

// kinds of objects to trigger reload
type configKind string

const (
	configKindCM     configKind = "conf:cm"
	configKindSecret configKind = "conf:secret"
)

type (
	reloadObjectKey struct {
		kind      reloadKind
		namespace string
		name      string
	}

	configRef struct {
		kind      configKind
		namespace string
		name      string
		key       string
	}
)

func (r reloadObjectKey) String() string {
	return string(r.kind) + "/ns:" + r.namespace + "/name:" + r.name
}

func (t configRef) String() string {
	ret := string(t.kind) + "/ns:" + t.namespace + "/name:" + t.name
	if t.key == "" {
		return ret
	}

	return ret + "/key:" + t.key
}

type (
	reloadSpec struct {
		reloadObjectKey
		triggers map[configRef]struct{}
	}

	syncerSpec struct {
		targetConfig configRef
		syncerConfig configRef
		syncer       *syncer.Syncer
	}
)

func createReloadKey(k reloadKind, namespace, name string) reloadObjectKey {
	return reloadObjectKey{
		kind:      k,
		namespace: namespace,
		name:      name,
	}
}

func createConfigRef(k configKind, namespace, name, key string) configRef {
	return configRef{
		kind:      k,
		namespace: namespace,
		name:      name,
		key:       key,
	}
}
