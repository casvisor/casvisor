FROM --platform=$BUILDPLATFORM node:20 AS FRONT
WORKDIR /web
COPY ./web/package.json ./web/yarn.lock ./
RUN yarn install --frozen-lockfile --network-timeout 1000000
COPY ./web .
RUN yarn run build


FROM --platform=$BUILDPLATFORM golang:1.23.6 AS BACK
WORKDIR /go/src/casvisor
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN chmod +x ./build.sh
RUN ./build.sh


FROM alpine:latest AS STANDARD
LABEL MAINTAINER="https://casvisor.org/"
ARG USER=casvisor
ARG TARGETOS
ARG TARGETARCH
ENV BUILDX_ARCH="${TARGETOS:-linux}_${TARGETARCH:-amd64}"
ENV sshGatewayPort=2222

RUN sed -i 's/https/http/' /etc/apk/repositories
RUN apk add --update sudo
RUN apk add curl
RUN apk add ca-certificates && update-ca-certificates

RUN adduser -D $USER -u 1000 \
    && echo "$USER ALL=(ALL) NOPASSWD: ALL" > /etc/sudoers.d/$USER \
    && chmod 0440 /etc/sudoers.d/$USER \
    && mkdir logs db recordings \
    && chown -R $USER:$USER logs db recordings

USER 1000
WORKDIR /
COPY --from=BACK --chown=$USER:$USER /go/src/casvisor/server_${BUILDX_ARCH} ./server
COPY --from=BACK --chown=$USER:$USER /go/src/casvisor/swagger ./swagger
COPY --from=BACK --chown=$USER:$USER /go/src/casvisor/data ./data
COPY --from=BACK --chown=$USER:$USER /go/src/casvisor/conf/app.conf ./conf/app.conf
COPY --from=FRONT --chown=$USER:$USER /web/build ./web/build

VOLUME /db /recordings
EXPOSE 19000 2222
ENTRYPOINT ["/server"]


FROM debian:bookworm-slim AS ALLINONE
LABEL MAINTAINER="https://casvisor.org/"
ARG TARGETOS
ARG TARGETARCH
ENV BUILDX_ARCH="${TARGETOS:-linux}_${TARGETARCH:-amd64}"
ENV sshGatewayPort=2222
# the all-in-one image keeps its own MySQL (MariaDB), as before
ENV driverName=mysql
ENV dataSourceName="root:123456@tcp(localhost:3306)/"
ENV dbName=casvisor

WORKDIR /

USER root
RUN apt-get update \
    && apt-get install -y      \
        mariadb-server         \
        mariadb-client         \
        ca-certificates        \
    && update-ca-certificates  \
    && rm -rf /var/lib/apt/lists/*

COPY --from=BACK /go/src/casvisor/server_${BUILDX_ARCH} ./server
COPY --from=BACK /go/src/casvisor/swagger ./swagger
COPY --from=BACK /go/src/casvisor/data ./data
COPY --from=BACK /go/src/casvisor/docker-entrypoint.sh /docker-entrypoint.sh
COPY --from=BACK /go/src/casvisor/conf/app.conf ./conf/app.conf
COPY --from=FRONT /web/build ./web/build

EXPOSE 19000 2222
ENTRYPOINT ["/bin/bash"]
CMD ["/docker-entrypoint.sh"]
