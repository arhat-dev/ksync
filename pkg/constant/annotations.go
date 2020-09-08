/*
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
*/

package constant

const (
	// AnnotationConfigMaps to specify workloads' configmaps to be watched
	//   e.g. ksync.arhat.dev/configmaps: ${NODE_NAME},foo,bar/key
	AnnotationConfigMaps = "ksync.arhat.dev/configmaps"

	// AnnotationSecrets to specify workloads' secrets to be watched
	AnnotationSecrets = "ksync.arhat.dev/secrets"

	// AnnotationForceConfigMaps additional configmaps to trigger reload
	AnnotationForceConfigMaps = "ksync.arhat.dev/force-configmaps"

	// AnnotationForceSecrets additional secrets to trigger reload
	AnnotationForceSecrets = "ksync.arhat.dev/force-secrets"

	// AnnotationSyncConfig to instruct controller how to sync config
	AnnotationSyncConfig = "ksync.arhat.dev/sync-config-ref"
)

const (
	AnnotationHashPrefix = "hash.ksync.arhat.dev"
)
