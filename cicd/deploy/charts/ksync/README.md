# ksync

`ksync` is a config sync and pod reloader for kubernetes

## Introduction

This is the official helm chart for [ksync](https://github.com/arhat-dev/ksync), you can deploy `ksync` to your clusters to sync your configmap/secrets wtih external sources and trigger pod reload after validation (fine grained)

## Prerequisites

- `helm` v3
- `Kubernetes` 1.15+

## Installing the Chart

```bash
helm install my-release arhat-dev/ksync
```

## Uninstalling the Chart

```bash
helm delete my-release
```

## Configuration

Please refer to the [`values.yaml`](https://github.com/arhat-dev/ksync/blob/master/cicd/deploy/charts/ksync/values.yaml)
