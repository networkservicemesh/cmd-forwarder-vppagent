# Intro

cmd-forwarder-vppagent sets out a pattern for building other cmds in networkservicemesh.  This pattern is expected to
shift and evolve as we learn new things and discover better techniques.

# Practices

## One cmd per repo

Each repo should have precisely one main package.  Generally, its best to keep that main package in the top level
directory so as to ease 'go get'-ing of the cmd.

## main.go

Try to keep to a single main.go for your cmd.  If main.go is growing larger than 100-200 lines of code, its probably indicates
you are trying to do to much in it and should either be writing bits in sdk/ or sdk-*/ or writing pkgs in ./internal.

The basic anatomy of a main.go should be:

1. Setup a context
2. Setup logging
3. Enable optional debugging
4. Extract configuration from environment
5. Get a TLSPeer
6. Setup any other needed things (vppagent if using it, authz policy etc)
7. Instantiate Endpoint
8. Create GRPC Server for endpoint
9. Wait for exit

### Setup Context

Create a new context with [signalctx](https://github.com/networkservicemesh/sdk/blob/master/pkg/tools/signalctx/context.go#L32) which will be cancelled if the cmd receives OS signals like SIGTERM:

```go
ctx := signalctx.WithSignals(context.Background())
```

Optionally add a cancel function that can be used to manually cancel subsequently.

```go
ctx, cancel := context.WithCancel(ctx)
```

### Setup logging

This would be the proper place and time to setup logrus Formatters, LogLevels, etc.
Minimally though, attach a 'cmd' field to the context:
```go
ctx = log.WithField(ctx, "cmd", os.Args[0])
```

### Enable optional debugging
[debug.Self()](https://github.com/networkservicemesh/sdk/blob/master/pkg/tools/debug/self.go#L53) makes it very easy to allow developers to optionally enable debugging with an Env Variable:
```go
if err := debug.Self(); err != nil {
	log.Entry(ctx).Infof("%s", err)
}
```

If the correct env variable is set, and dlv is installed, this will cause the cmd to exec dlv to debug itself.
If there is something that prevents debugging, the error message logged will provide sufficient info to enable doing so.

### Extract configuration from environment

The simplest way to extract configuration from the environment is [envconfig](https://github.com/kelseyhightower/envconfig).
It allows you to create a simple struct for your configuration, and in can unmarshal that from env variables.
envconfig's [supported field types](https://github.com/kelseyhightower/envconfig#supported-struct-field-types) include
anything that implements [encoding.TextUnmarshaler](https://golang.org/pkg/encoding/#TextUnmarshaler) and 
[encoding.BinaryUnmarshaler](https://golang.org/pkg/encoding/#BinaryUnmarshaler).  This allows native use of handy things
like url.URL and net.IP.  envconfig also interprets arrays as meaning that the env variable is a comma delimited list
of the type of the array.

[Example Config struct](https://github.com/networkservicemesh/cmd-forwarder-vppagent/blob/master/main.go#L46):
```go
type Config struct {
	Name             string        `default:"forwarder" desc:"Name of Endpoint"`
	BaseDir          string        `default:"./" desc:"base directory" split_words:"true"`
	TunnelIP         net.IP        `desc:"IP to use for tunnels" split_words:"true"`
	ListenOn         url.URL       `default:"unix:///listen.on.socket" desc:"url to listen on" split_words:"true"`
	ConnectTo        url.URL       `default:"unix:///connect.to.socket" desc:"url to connect to" split_words:"true"`
	MaxTokenLifetime time.Duration `default:"24h" desc:"maximum lifetime of tokens" split_words:"true"`
}
```

The config can be [extracted from the env](https://github.com/networkservicemesh/cmd-forwarder-vppagent/blob/9a981c41e568582c3916f8143a826edcd7d8162e/main.go#L73) with:

```go
config := &Config{}
if err := envconfig.Process("nsm", config); err != nil {
	logrus.Fatalf("error processing config from env: %+v", err)
}
```

It is recommended you also [output the usage by default](https://github.com/networkservicemesh/cmd-forwarder-vppagent/blob/9a981c41e568582c3916f8143a826edcd7d8162e/main.go#L75):

```go
if err := envconfig.Usage("nsm", config); err != nil {
	logrus.Fatal(err)
}
```

to make it easier for users to see what their options are.  If at all possible, please provide either a usable default value
or intolerance to no value.  If a value truly is required, use the ['required' tag](https://github.com/kelseyhightower/envconfig#struct-tag-support).

### Get a TLSPeer
[Example](https://github.com/networkservicemesh/cmd-forwarder-vppagent/blob/9a981c41e568582c3916f8143a826edcd7d8162e/main.go#L88):
```go
tlsPeer, err := spiffeutils.NewTLSPeer()
if err != nil {
	log.Entry(ctx).Fatalf("Error attempting to create spiffeutils.TLSPeer %+v", err)
}
```

Please utilize [spiffeutils](https://github.com/networkservicemesh/sdk/tree/9a981c41e568582c3916f8143a826edcd7d8162e/pkg/tools/spiffeutils) to get a TLSPeer that is resilient to the absense of a Spire server.  TLSPeer will
be used subsequently as Credentials for GRPC Listening and grpc.ClientConn creation.

### Setup other needed things

If you require a [vpp-agent](https://github.com/ligato/vpp-agent),
[Example](https://github.com/networkservicemesh/cmd-forwarder-vppagent/blob/9a981c41e568582c3916f8143a826edcd7d8162e/main.go#L84):
```go
vppagentCC, vppagentErrCh := vppagent.StartAndDialContext(ctx)
	exitOnErr(ctx, cancel, vppagentErrCh)
```

### Instantiate Endpoint

Endpoint instantiation should be simple, generally it will use a chain defined in some sdk*/ repo.
All configuration parameters should be derived from the config extracted from the environment.

[Example](https://github.com/networkservicemesh/cmd-forwarder-vppagent/blob/9a981c41e568582c3916f8143a826edcd7d8162e/main.go#L100):
```go
endpoint := xconnectns.NewServer(
		config.Name,
		&authzPolicy,
		spiffeutils.SpiffeJWTTokenGeneratorFunc(tlsPeer.GetCertificate, config.MaxTokenLifetime),
		vppagentCC,
		config.BaseDir,
		config.TunnelIP,
		vppinit.Func(config.TunnelIP),
		&config.ConnectTo,
		spiffeutils.WithSpiffe(tlsPeer, time.Second),
		grpc.WithDefaultCallOptions(grpc.WaitForReady(true)),
	)
```
### Create GRPC Server for endpoint

[Example](https://github.com/networkservicemesh/cmd-forwarder-vppagent/blob/9a981c41e568582c3916f8143a826edcd7d8162e/main.go#L114):
```go
server := grpc.NewServer(spiffeutils.SpiffeCreds(tlsPeer, time.Second))
endpoint.Register(server)
srvErrCh := grpcutils.ListenAndServe(ctx, &config.ListenOn, server)
```

### Wait for exit 

[Example](https://github.com/networkservicemesh/cmd-forwarder-vppagent/blob/9a981c41e568582c3916f8143a826edcd7d8162e/main.go#L122):
```go
<-ctx.Done()
<-vppagentErrCh
```

## ./internal

Any pkgs that are local to the repo should be put in ./internal.  If you think those packages should be
shared with others, then those packages probably belong in an sdk*/ repo.

In the example of cmd-forwarder-vppagent we have three 'internal' packages:

1. ./internal/authz - a helper for authz until a more elegant solution can be provided
2. ./internal/imports - a simple package used for priming Docker builds to maximize layer caching
3. ./internal/vppinit - an implementation of the vppinit function that is specific to this cmd

## Dockerfile

Dockerfiles should be kept as simple and highly patterned as possible.  Generally speaking in order to build, test, and debug
most Network Service Mesh cmds will require:

1. Go - to build
2. Spire - needed to test
3. Dlv - neede to debug
   
   and then
4. Build
5. Test
6. Debug
7. Runtime


### Go
If you are working on Network Service Mesh cmds that do not require vppagent, you can usually simply
use a [golang base docker image](https://hub.docker.com/_/golang) to get Go.  However, in the case of 
vppagent based Network Service Mesh Commands you probably will be using a ligato base image, and can thus install
go using:

```dockerfile
FROM ligato/vpp-agent:v3.1.0 as go
COPY --from=golang:1.13.8-stretch /usr/local/go/ /go
ENV PATH ${PATH}:/go/bin
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOBIN=/bin
```


### Spire
To add spire to the image:
```dockerfile
RUN go run github.com/edwarnicke/dl \
    https://github.com/spiffe/spire/releases/download/v0.9.3/spire-0.9.3-linux-x86_64-glibc.tar.gz | \
    tar -xzvf - -C /bin --strip=3 ./spire-0.9.3/bin/spire-server ./spire-0.9.3/bin/spire-agent
```

### Dlv
To add dlv to the image:
```dockerfile
RUN go get github.com/go-delve/delve/cmd/dlv@v1.4.0
```

### Build

Your build stage will look something like
```dockerfile
WORKDIR /build
COPY go.mod go.sum ./
COPY ./local ./local
COPY ./internal/imports ./internal/imports
RUN go build ./internal/imports
COPY . .
RUN go build -o /bin/forwarder .
```

COPYing go.mod, go.sum, and ./internal/imports in allows us to do a RUN of go build ./internal/imports.
Building that package will:
a. Download all dependencies
b. Build all packages imported anywhere in the go module that are not provided by the go module.
These prime both the source and binary cache in Docker, so that you get extremely fast (usually within a few hundred milliseconds of host native)
builds in docker.

COPYing in ./local allows us to bring along any local 'replace' directives from our go.mod file, thus permitting Docker builds to continue
working even if we are working on local copies of dependencies of the cmd module.

### Test
Because it is often the case that we need an entire system of things (Spire etc) and some of those things may not
run on our native host development boxes, we would like Docker to enable us to test.  This is done with a simple layer:

```dockerfile
FROM build as test
CMD go test ./...
```

In this way, we wind up with a Docker target we can use to run all tests using:

```bash
docker run --rm $(docker build -q --target test .)
```

### Debug
It is extremely useful to be able to debug tests.  This is simple to enable with an additional target:
```dockerfile
FROM test as debug
CMD dlv -l :40000 --headless=true --api-version=2 test -test.v ./...
```

which allows use of:
```bash
docker run --rm -p 40000:40000 $(docker build -q --target debug .)
```

to debug tests.

### Runtime

Finally, simply copy the binary into a base image for runtime:
```dockerfile
FROM ligato/vpp-agent:v3.1.0 as runtime
COPY --from=build /bin/forwarder /bin/forwarder
CMD /bin/forwarder
```

## ./local

Sometimes, you need to work on other go modules on which the cmd depends. For example, cmd-forwarder-vppagent
depends on both github.com/networkservicemesh/sdk and github.com/networkservicemesh/sdk-vppagent.  If in the course
of doing development work on it.  For this reason we have a standard place for such work, with a .gitignore to 
ensure that such things are not checked in by mistake.



