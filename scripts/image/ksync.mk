# Copyright 2020 The arhat.dev Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# build
image.build.ksync.linux.x86:
	sh scripts/image/build.sh $@

image.build.ksync.linux.amd64:
	sh scripts/image/build.sh $@

image.build.ksync.linux.armv6:
	sh scripts/image/build.sh $@

image.build.ksync.linux.armv7:
	sh scripts/image/build.sh $@

image.build.ksync.linux.arm64:
	sh scripts/image/build.sh $@

image.build.ksync.linux.ppc64le:
	sh scripts/image/build.sh $@

image.build.ksync.linux.s390x:
	sh scripts/image/build.sh $@

image.build.ksync.linux.all: \
	image.build.ksync.linux.amd64 \
	image.build.ksync.linux.arm64 \
	image.build.ksync.linux.armv7 \
	image.build.ksync.linux.armv6 \
	image.build.ksync.linux.x86 \
	image.build.ksync.linux.s390x \
	image.build.ksync.linux.ppc64le

image.build.ksync.windows.amd64:
	sh scripts/image/build.sh $@

image.build.ksync.windows.armv7:
	sh scripts/image/build.sh $@

image.build.ksync.windows.all: \
	image.build.ksync.windows.amd64 \
	image.build.ksync.windows.armv7

# push
image.push.ksync.linux.x86:
	sh scripts/image/push.sh $@

image.push.ksync.linux.amd64:
	sh scripts/image/push.sh $@

image.push.ksync.linux.armv6:
	sh scripts/image/push.sh $@

image.push.ksync.linux.armv7:
	sh scripts/image/push.sh $@

image.push.ksync.linux.arm64:
	sh scripts/image/push.sh $@

image.push.ksync.linux.ppc64le:
	sh scripts/image/push.sh $@

image.push.ksync.linux.s390x:
	sh scripts/image/push.sh $@

image.push.ksync.linux.all: \
	image.push.ksync.linux.amd64 \
	image.push.ksync.linux.arm64 \
	image.push.ksync.linux.armv7 \
	image.push.ksync.linux.armv6 \
	image.push.ksync.linux.x86 \
	image.push.ksync.linux.s390x \
	image.push.ksync.linux.ppc64le

image.push.ksync.windows.amd64:
	sh scripts/image/push.sh $@

image.push.ksync.windows.armv7:
	sh scripts/image/push.sh $@

image.push.ksync.windows.all: \
	image.push.ksync.windows.amd64 \
	image.push.ksync.windows.armv7
