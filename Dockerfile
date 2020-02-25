# Set --build-args BUILD=false to copy in binaries built on host
# To Build on host start:
# CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o . ./...
# Build all test commands as well
# CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c ./src/forwarder
FROM golang:1.13.8-alpine3.11 as build
WORKDIR /build
RUN apk add file
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GO111MODULE=on go get github.com/go-delve/delve/cmd/dlv@v1.4.0
RUN wget -O - https://github.com/spiffe/spire/releases/download/v0.9.3/spire-0.9.3-linux-x86_64-glibc.tar.gz | tar -C /opt -xz
COPY go.mod .
COPY go.sum .
ARG BUILD=true
RUN  [ "${BUILD}" != "true" ] ||  go mod download
COPY . .
RUN  [ "${BUILD}" != "true" ] || CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o . ./...
RUN file forwarder | grep "ELF 64-bit LSB executable, x86-64" || (echo "Compile with: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o . ./..." && exit 1 )
RUN  [ "${BUILD}" != "true" ] || (grep -l -r "func Test.*(.*\*testing.T)" . | xargs -n1 dirname | CGO_ENABLED=0 GOOS=linux GOARCH=amd64  xargs -n1 go test -c)
RUN file forwarder.test | grep -q "ELF 64-bit LSB executable, x86-64" || (echo "Compile with: CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c ./src/forwarder"; exit 1)

# Copy the results of the build into the runtime container
FROM ligato/vpp-agent:v3.1.0 as runtime
COPY --from=build /build/forwarder /bin
CMD ["/bin/forwarder", "run"]

FROM runtime as test
COPY --from=build /go/bin/dlv /bin/dlv
COPY --from=build /opt/spire-*/bin /bin
COPY --from=build /build/*.test /bin/
CMD ["/bin/forwarder", "test"]

FROM runtime
