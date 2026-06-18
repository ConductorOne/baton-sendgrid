package main

import (
	"context"

	sdkconfig "github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorrunner"
	"github.com/conductorone/baton-sendgrid/pkg/config"
	"github.com/conductorone/baton-sendgrid/pkg/connector"
)

const (
	version       = "dev"
	connectorName = "baton-sendgrid"
)

func main() {
	ctx := context.Background()
	sdkconfig.RunConnector(ctx,
		connectorName,
		version,
		config.Config,
		connector.NewLambdaConnector,
		connectorrunner.WithDefaultCapabilitiesConnectorBuilderV2(&connector.Connector{}),
	)
}
