package controller

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"arhat.dev/ksync/pkg/constant"
)

type nameKeyPair struct {
	name, key string
}

func getNameAndOptionalKey(names string) []nameKeyPair {
	var result []nameKeyPair

	for _, nk := range strings.Split(names, ",") {
		parts := strings.SplitN(nk, "/", 2)
		switch len(parts) {
		case 1:
			// name only
			result = append(result, nameKeyPair{name: parts[0]})
		case 2:
			// name and key
			result = append(result, nameKeyPair{name: parts[0], key: parts[1]})
		}
	}

	return result
}

func isConfigRequireSynced(md metav1.ObjectMetaAccessor) bool {
	labels := md.GetObjectMeta().GetLabels()

	if len(labels) == 0 {
		return false
	}

	return labels[constant.LabelAction] == constant.LabelActionValueSync
}
