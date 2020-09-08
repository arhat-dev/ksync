package controller

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	podshelper "k8s.io/kubernetes/pkg/apis/core/pods"
	"k8s.io/kubernetes/pkg/fieldpath"
	kubecontainer "k8s.io/kubernetes/pkg/kubelet/container"
	"k8s.io/kubernetes/third_party/forked/golang/expansion"
)

// nolint:gocyclo
func (c *Controller) expandContainersEnvs(pod *v1.Pod, container *v1.Container) ([]kubecontainer.EnvVar, error) {
	var (
		configMaps = make(map[string]*v1.ConfigMap)
		secrets    = make(map[string]*v1.Secret)
		tmpEnv     = make(map[string]string)
		result     []kubecontainer.EnvVar
		err        error
	)

	// Env will override EnvFrom variables.
	// Process EnvFrom first then allow Env to replace existing values.
	for _, envFrom := range container.EnvFrom {
		switch {
		case envFrom.ConfigMapRef != nil:
			cm := envFrom.ConfigMapRef
			name := cm.Name
			configMap, ok := configMaps[name]
			if !ok {
				optional := cm.Optional != nil && *cm.Optional

				var (
					cmObj     interface{}
					found, ok bool
				)
				cmObj, found, err = c.cmInformer.GetIndexer().GetByKey(pod.Namespace + "/" + name)
				if err != nil {
					return result, err
				}
				if !found && optional {
					// ignore error when marked optional
					continue
				}
				configMap, ok = cmObj.(*v1.ConfigMap)
				if !ok {
					return result, fmt.Errorf("failed to convert to configmap type")
				}

				configMaps[name] = configMap
			}

			for k, v := range configMap.Data {
				if len(envFrom.Prefix) > 0 {
					k = envFrom.Prefix + k
				}
				if errMsgs := utilvalidation.IsEnvVarName(k); len(errMsgs) != 0 {
					continue
				}
				tmpEnv[k] = v
			}
		case envFrom.SecretRef != nil:
			s := envFrom.SecretRef
			name := s.Name
			secret, ok := secrets[name]
			if !ok {
				optional := s.Optional != nil && *s.Optional

				var (
					secretObj interface{}
					found, ok bool
				)
				secretObj, found, err = c.secretInformer.GetIndexer().GetByKey(pod.Namespace + "/" + name)
				if err != nil {
					return result, err
				}
				if !found && optional {
					// ignore error when marked optional
					continue
				}
				secret, ok = secretObj.(*v1.Secret)
				if !ok {
					return result, fmt.Errorf("failed to convert to secret type")
				}
				secrets[name] = secret
			}

			for k, v := range secret.Data {
				if len(envFrom.Prefix) > 0 {
					k = envFrom.Prefix + k
				}
				if errMsgs := utilvalidation.IsEnvVarName(k); len(errMsgs) != 0 {
					continue
				}
				tmpEnv[k] = string(v)
			}
		}
	}

	// Determine the final values of variables:
	//
	// 1.  Determine the final value of each variable:
	//     a.  If the variable's Value is set, expand the `$(var)` references to other
	//         variables in the .Value field; the sources of variables are the declared
	//         variables of the container and the service environment variables
	//     b.  If a source is defined for an environment variable, resolve the source
	// 2.  Create the container's environment in the order variables are declared
	// 3.  Add remaining service environment vars
	var (
		mappingFunc = expansion.MappingFuncFor(tmpEnv)
	)
	for _, envVar := range container.Env {
		runtimeVal := envVar.Value
		if runtimeVal != "" {
			// Step 1a: expand variable references
			runtimeVal = expansion.Expand(runtimeVal, mappingFunc)
		} else if envVar.ValueFrom != nil {
			// Step 1b: resolve alternate env var sources
			switch {
			case envVar.ValueFrom.FieldRef != nil:
				var podIPs []string
				for _, p := range pod.Status.PodIPs {
					podIPs = append(podIPs, p.IP)
				}
				runtimeVal, err = podFieldSelectorRuntimeValue(envVar.ValueFrom.FieldRef, pod, pod.Status.PodIP, podIPs)
				if err != nil {
					return result, err
				}
			case envVar.ValueFrom.ResourceFieldRef != nil:
				//defaultedPod, defaultedContainer, err := kl.defaultPodLimitsForDownwardAPI(pod, container)
				//if err != nil {
				//	return result, err
				//}
				//runtimeVal, err = containerResourceRuntimeValue(envVar.ValueFrom.ResourceFieldRef,
				// 												 defaultedPod, defaultedContainer)
				//if err != nil {
				//	return result, err
				//}
			case envVar.ValueFrom.ConfigMapKeyRef != nil:
				cm := envVar.ValueFrom.ConfigMapKeyRef
				name := cm.Name
				key := cm.Key
				optional := cm.Optional != nil && *cm.Optional
				configMap, ok := configMaps[name]
				if !ok {
					var (
						cmObj interface{}
						found bool
					)
					cmObj, found, err = c.cmInformer.GetIndexer().GetByKey(pod.Namespace + "/" + name)
					if err != nil {
						return result, err
					}
					if !found && optional {
						// ignore error when marked optional
						continue
					}
					configMap, ok = cmObj.(*v1.ConfigMap)
					if !ok {
						return result, fmt.Errorf("failed to convert to configmap type")
					}
					configMaps[name] = configMap
				}
				runtimeVal, ok = configMap.Data[key]
				if !ok {
					if optional {
						continue
					}
					return result, fmt.Errorf("couldn't find key %v in ConfigMap %v/%v", key, pod.Namespace, name)
				}
			case envVar.ValueFrom.SecretKeyRef != nil:
				s := envVar.ValueFrom.SecretKeyRef
				name := s.Name
				key := s.Key
				optional := s.Optional != nil && *s.Optional
				secret, ok := secrets[name]
				if !ok {
					var (
						secretObj interface{}
						found     bool
					)
					secretObj, found, err = c.secretInformer.GetIndexer().GetByKey(pod.Namespace + "/" + name)
					if err != nil {
						return result, err
					}
					if !found && optional {
						// ignore error when marked optional
						continue
					}
					secret, ok = secretObj.(*v1.Secret)
					if !ok {
						return result, fmt.Errorf("failed to convert to secret type")
					}
					secrets[name] = secret
				}
				var runtimeValBytes []byte
				runtimeValBytes, ok = secret.Data[key]
				if !ok {
					if optional {
						continue
					}
					return result, fmt.Errorf("couldn't find key %v in Secret %v/%v", key, pod.Namespace, name)
				}
				runtimeVal = string(runtimeValBytes)
			}
		}

		tmpEnv[envVar.Name] = runtimeVal
	}

	// Append the env vars
	for k, v := range tmpEnv {
		result = append(result, kubecontainer.EnvVar{Name: k, Value: v})
	}

	return result, nil
}

func podFieldSelectorRuntimeValue(
	fs *v1.ObjectFieldSelector,
	pod *v1.Pod, podIP string,
	podIPs []string,
) (string, error) {
	internalFieldPath, _, err := podshelper.ConvertDownwardAPIFieldLabel(fs.APIVersion, fs.FieldPath, "")
	if err != nil {
		return "", err
	}
	switch internalFieldPath {
	case "spec.nodeName":
		return pod.Spec.NodeName, nil
	case "spec.serviceAccountName":
		return pod.Spec.ServiceAccountName, nil
	case "status.hostIP":
		return pod.Status.HostIP, nil
	case "status.podIP":
		return podIP, nil
	case "status.podIPs":
		return strings.Join(podIPs, ","), nil
	}
	return fieldpath.ExtractFieldPathAsString(pod, internalFieldPath)
}

// func containerResourceRuntimeValue(
// 	fs *v1.ResourceFieldSelector,
// 	pod *v1.Pod,
// 	container *v1.Container,
// ) (string, error) {
// 	containerName := fs.ContainerName
// 	if len(containerName) == 0 {
// 		return resource.ExtractContainerResourceValue(fs, container)
// 	}
// 	return resource.ExtractResourceValueByContainerName(fs, pod, containerName)
// }
