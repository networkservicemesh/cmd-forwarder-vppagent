module github.com/networkservicemesh/cmd-forwarder-vppagent

go 1.13

require (
	github.com/antonfisher/nested-logrus-formatter v1.0.3
	github.com/edwarnicke/exechelper v1.0.2
	github.com/edwarnicke/grpcfd v0.0.0-20200920223154-d5b6e1f19bd0
	github.com/golang/protobuf v1.4.2
	github.com/kelseyhightower/envconfig v1.4.0
	github.com/networkservicemesh/api v0.0.0-20201001183932-93ee44ca6fc4
	github.com/networkservicemesh/sdk v0.0.0-20201002042747-d2870b8f5ceb
	github.com/networkservicemesh/sdk-vppagent v0.0.0-20201002043126-d91e4d284c32
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.6.0
	github.com/spiffe/go-spiffe/v2 v2.0.0-alpha.4.0.20200528145730-dc11d0c74e85
	github.com/stretchr/testify v1.6.1
	github.com/vishvananda/netlink v0.0.0-20180910184128-56b1bd27a9a3
	github.com/vishvananda/netns v0.0.0-20200728191858-db3c7e526aae
	go.ligato.io/vpp-agent/v3 v3.1.0
	golang.org/x/sys v0.0.0-20200916084744-dbad9cb7cb7a
	google.golang.org/grpc v1.32.0
)
