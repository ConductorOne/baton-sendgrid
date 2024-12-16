package main

import (
	"context"
	"fmt"
	"os"

	"github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/types"
	config2 "github.com/conductorone/baton-sendgrid/cmd/baton-sendgrid/config"
	"github.com/conductorone/baton-sendgrid/pkg/connector"
	"github.com/conductorone/baton-sendgrid/pkg/connector/client"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var version = "dev"

func main() {
	ctx := context.Background()

	_, cmd, err := config.DefineConfiguration(
		ctx,
		"baton-sendgrid",
		getConnector,
		field.Configuration{
			Fields: config2.ConfigurationFields,
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	cmd.Version = version

	err = cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func getConnector(ctx context.Context, v *viper.Viper) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)
	if err := config2.ValidateConfig(v); err != nil {
		return nil, err
	}

	sendGridApyKey := v.GetString(config2.SendGridApiKeyField.GetName())
	sendgridRegion := v.GetString(config2.SendGridRegionField.GetName())
	sendgridIgnoreSubusers := v.GetBool(config2.IgnoreSubusers.GetName())

	var baseUrl string

	switch sendgridRegion {
	case "eu":
		baseUrl = client.SendGridEUBaseUrl
	case "global":
		baseUrl = client.SendGridBaseUrl
	default:
		baseUrl = client.SendGridBaseUrl
		l.Warn("invalid sendgrid region", zap.String("region", sendgridRegion))
	}

	sendGridCliet, err := client.NewClient(ctx, baseUrl, sendGridApyKey)
	if err != nil {
		l.Error("error creating sendgrid client", zap.Error(err))
		return nil, err
	}

	cb, err := connector.New(ctx, sendGridCliet, sendgridIgnoreSubusers)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}
	connector, err := connectorbuilder.NewConnector(ctx, cb)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}
	return connector, nil
}
