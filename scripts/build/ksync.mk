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

# native
ksync:
	sh scripts/build/build.sh $@

# linux
ksync.linux.amd64:
	sh scripts/build/build.sh $@

ksync.linux.arm64:
	sh scripts/build/build.sh $@

ksync.linux.armv7:
	sh scripts/build/build.sh $@

ksync.linux.armv6:
	sh scripts/build/build.sh $@

ksync.linux.armv5:
	sh scripts/build/build.sh $@

ksync.linux.x86:
	sh scripts/build/build.sh $@

ksync.linux.ppc64le:
	sh scripts/build/build.sh $@

ksync.linux.mips64le:
	sh scripts/build/build.sh $@

ksync.linux.s390x:
	sh scripts/build/build.sh $@

ksync.linux.all: \
	ksync.linux.amd64 \
	ksync.linux.arm64 \
	ksync.linux.armv7 \
	ksync.linux.armv6 \
	ksync.linux.armv5 \
	ksync.linux.x86 \
	ksync.linux.ppc64le \
	ksync.linux.mips64le \
	ksync.linux.s390x

# windows
ksync.windows.amd64:
	sh scripts/build/build.sh $@

ksync.windows.armv7:
	sh scripts/build/build.sh $@

ksync.windows.all: \
	ksync.windows.amd64 \
	ksync.windows.armv7
