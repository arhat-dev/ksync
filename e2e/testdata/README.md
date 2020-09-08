# Test data Usage

- [`01-config-data.yaml`](./01-config-data.yaml) - Dummy ConfigMaps and Secrets for test purpose
- [`10-daemonset.yaml`](./10-daemonset.yaml) - Example to use custom annotations to get specific pod(s) reloaded

## Explanations

### `10-daemonset.yaml`

The DaemonSet contains no explicit volume mount using ConfigMap/Secret, but we can reload it with ksync

The most import part is its labels and annotations:

```yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  labels:
    ksync.arhat.dev/action: reload
  annotations:
    ksync.arhat.dev/configmaps: $(NODE_NAME)/$(NODE_NAME).yaml,foo
    ksync.arhat.dev/secrets: foo
```

- Label `ksync.arhat.dev/action: reload` instructs the ksync to register this daemonset as reloadable

**NOTE:** label key and value are not customizable (for now)

- Annotation `ksync.arhat.dev/configmaps: $(NODE_NAME)/$(NODE_NAME).yaml,foo` contains two configmap dependencies

  - first one with name `$(NODE_NAME)` and key `$(NODE_NAME).yaml` (separated by `/`)
    - `$(NODE_NAME)` or any name or key including `$()` will be evaluated per pod with its containers env vars
    - let's assume our `$(NODE_NAME)` is `kube-node-1`, then if you want to trigger reload using first configmap, you need to change data with key `kube-node-1.yaml` inside configmap `kube-node-1`

  - second one with name `foo` and no key specified
    - once you changed any data in the configmap `foo`, it will trigger a update on the whole daemonset (due to no variable)

- Annotation `ksync.arhat.dev/secrets: foo` is the same as to the `foo` example in configmap, but intended for secrets

**NOTE:** If you have defined multiple environment variable with the same name in different containers in a single pod, the final environment variable's value will be used to eval `$()` expressions
