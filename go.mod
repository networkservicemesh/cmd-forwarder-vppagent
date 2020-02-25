module github.com/networkservicemesh/cmd-forwarder-vppagent

go 1.13

require (
	github.com/networkservicemesh/api v0.0.0-20200223155536-6728cf448703
	github.com/networkservicemesh/sdk-vppagent v0.0.0-20200224235828-3983c1bc3b12
	github.com/sirupsen/logrus v1.4.2
	github.com/spf13/viper v1.6.2
	google.golang.org/grpc v1.27.0
)

replace github.com/satori/go.uuid => github.com/satori/go.uuid v1.2.1-0.20181028125025-b2ce2384e17b
