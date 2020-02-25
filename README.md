# Intro

This repo contains 'forwarder' that implements the xconnect Network Service using vppvagent.

This README will provide directions for building, testing, and debugging that container.

# Build
## Build forwarder binary locally

You can build the forwarder binary locally by executing

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o . ./...
```

## Build Docker container

You can build the docker container by running:

```bash
docker build .
```

## Build Docker container using locally built forwarder

During development, it is often useful to build a docker container based on your locally built binary.
To do that run:

```bash
docker build --build-arg BUILD=false .
```

Please note, this presumes you followed instructions above and built your local binary with 

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o . ./...
```

# Testing
## Testing Docker container

Testing is run via a Docker container.  To run testing run:

```bash
docker run --rm $(docker build -q --target test .)
```

If you want to poke around in the container after the tests simply add a '-it' and you will be dumped into a bash
shell inside the container after the tests run.
```bash
docker run --rm -it $(docker build -q --target test .)
```

## Testing Docker container with locally built forwarder and tests

Execute the following to build locally the forwarder, forwarder.test, create the docker test container and run it.

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o . ./... &&\
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c ./src/forwarder &&\
docker run --rm $(docker build -q --build-arg BUILD=false --target test .)
```

# Debugging

When you run 'forwarder run' you will see an early line of output that tells you:

```Setting env variable DLV_LISTEN_FORWARDER to a valid dlv '--listen' value will cause the dlv debugger to execute this binary and listen as directed.```

If you follow those instructions when running the Docker container:
```bash
docker run -e DLV_LISTEN_FORWARDER=:40000 -p 40000:40000 --rm $(docker build -q --target test .)
```

```-e DLV_LISTEN_FORWARDER=:40000``` tells docker to set the environment variable DLV_LISTEN_FORWARDER to :40000 telling
dlv to listen on port 40000.

```-p 40000:40000``` tells docker to forward port 40000 in the container to port 40000 in the host.  From there, you can
just connect dlv using your favorite IDE and debug forwarder.

## Debugging with host local build
This also works in combination with local building of the binaries:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o . ./... &&\
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c ./src/forwarder &&\
docker run -e DLV_LISTEN_FORWARDER=:40000 -p 40000:40000 --rm $(docker build -q --build-arg BUILD=false .)
```

## Debugging forwarder with tests
Or with your tests:
```bash
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o . ./... &&\
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c ./src/forwarder &&\
docker run -e DLV_LISTEN_FORWARDER=:40000 -p 40000:40000 --rm $(docker build -q --build-arg BUILD=false --target test .)
```
