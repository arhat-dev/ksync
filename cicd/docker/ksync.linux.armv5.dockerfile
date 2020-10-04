ARG ARCH=armv5

FROM arhatdev/builder-go:alpine as builder
FROM arhatdev/go:debian-${ARCH}
ARG APP=ksync

ENTRYPOINT [ "/ksync" ]
