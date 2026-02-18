package main

import (
	"context"

	cfg "github.com/conductorone/baton-sendgrid/pkg/config"
	"github.com/conductorone/baton-sendgrid/pkg/connector"
	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorrunner"
)

var version = "dev"

func main() {
	ctx := context.Background()
	config.RunConnector(ctx,
		"baton-sendgrid",
		version,
		cfg.Config,
		connector.New,
		connectorrunner.WithSessionStoreEnabled(),
		connectorrunner.WithDefaultCapabilitiesConnectorBuilderV2(&connector.Connector{}),
	)
}
