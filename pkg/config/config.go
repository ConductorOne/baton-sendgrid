package config

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	SendGridApiKeyField = field.StringField(
		"sendgrid-api-key",
		field.WithDisplayName("API Key"),
		field.WithRequired(true),
		field.WithIsSecret(true),
		field.WithDescription("API key for SendGrid service."),
	)

	SendGridRegionField = field.SelectField(
		"sendgrid-region",
		[]string{"global", "eu"},
		field.WithDisplayName("Region"),
		field.WithRequired(false),
		field.WithDefaultValue("global"),
		field.WithDescription("Region for SendGrid service."),
	)

	IgnoreSubusers = field.BoolField(
		"ignore-subusers",
		field.WithDisplayName("Ignore Subusers"),
		field.WithDefaultValue(false),
		field.WithDescription("Ignore subusers in the SendGrid account, subusers are an upgraded feature of sendgrid."),
	)
)

var (
	// ConfigurationFields defines the external configuration required for the
	// connector to run. Note: these fields can be marked as optional or
	// required.
	ConfigurationFields = []field.SchemaField{
		SendGridApiKeyField,
		SendGridRegionField,
		IgnoreSubusers,
	}

)

//go:generate go run ./gen
var Config = field.NewConfiguration(
	ConfigurationFields,
	field.WithConnectorDisplayName("SendGrid"),
	field.WithHelpUrl("/docs/baton/sendgrid"),
	field.WithIconUrl("/static/app-icons/sendgrid.svg"),
)
