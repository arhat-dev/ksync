# ksync

[![CI](https://github.com/arhat-dev/ksync/workflows/CI/badge.svg)](https://github.com/arhat-dev/ksync/actions?query=workflow%3ACI)
[![PkgGoDev](https://pkg.go.dev/badge/arhat.dev/ksync)](https://pkg.go.dev/arhat.dev/ksync)
[![GoReportCard](https://goreportcard.com/badge/arhat.dev/ksync)](https://goreportcard.com/report/arhat.dev/ksync)
[![codecov](https://codecov.io/gh/arhat-dev/ksync/branch/master/graph/badge.svg)](https://codecov.io/gh/arhat-dev/ksync)

A Kubernetes config sync controller and workload reloader

## Features

- [x] Fine-grained pod reload when config (`ConfigMap`, `Secret`) changed
- [x] `ConfigMap`/`Secret` data sync

## Usage: Reload

- Label `Deployment`/`Daemonset`/`Statefulset` to get enable reload on config updated

  ```bash
  kubectl label {deploy|ds|sts} <resource-name> ksync.arhat.dev/action="reload"
  ```

- (Optional) Select configmaps to be managed, comma seperated name list (if not specified, all configmaps mounted in the pod can trigger reload)

  ```bash
  kubectl annotate {deploy|ds|sts} <resource-name> ksync.arhat.dev/configmaps="foo,bar"
  ```

- (Optional) Select secrets to be managed, comma seperated name list (if not specified, all secrets mounted in the pod can trigger reload)

  ```bash
  kubectl annotate {deploy|ds|sts} <resource-name> ksync.arhat.dev/secrets="foo,bar"
  ```

**NOTICE:** For more examples, please refer to manifests and guides in [test/testdata](./test/testdata)

## Usage: Config Sync

- Label `ConfigMap`/`Secret` for syncing

  ```bash
  kubectl label {cm|secrets} <resource-name> ksync.arhat.dev/action="sync"
  ```

- Create a config for config sync inside `ConfigMap`/`Secret` (please refer to [`config.sync.yaml`](./config.sync.yaml) for config example)

  ```bash
  kubectl create {cm|secrets general} <my-sync-config-name> --from-file config.sync.yaml
  ```

- Annotate the `ConfigMap`/`Secret` to be synced with the sync config just created (refered sync config will reload syncer automatically)

  ```bash
  # namespace is not requred if the sync config resource is in the same namespace with the one to be synced
  kubectl annotate {cm|secrets} <resource-name> ksync.arhat.dev/sync-config-ref="{configmap|secret}://{ | <namespace>/}<name>/<key>"
  ```

## LICENSE

```text
Copyright 2020 The arhat.dev Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```
