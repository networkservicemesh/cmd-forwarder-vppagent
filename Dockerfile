FROM golang:1.13.8-alpine3.11 as build
WORKDIR /build
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY src/ src/
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...

FROM artembelov/vpp-agent:v2.5.1 as runtime
RUN rm /opt/vpp-agent/dev/etcd.conf; echo "disabled: true" > /opt/vpp-agent/dev/linux-plugin.conf;echo 'Endpoint: "localhost:9111"' > /opt/vpp-agent/dev/grpc.conf
RUN  mkdir -p /run/vpp
RUN  mkdir -p /tmp/vpp/
COPY conf/vpp/startup.conf /etc/vpp/vpp.conf
COPY conf/supervisord/supervisord.conf /opt/vpp-agent/dev/supervisor.conf
COPY conf/supervisord/govpp.conf /opt/vpp-agent/dev/govpp.conf
COPY --from=build /build/forwarder-vppagent /bin/
