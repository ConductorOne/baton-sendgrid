package config

import "github.com/conductorone/baton-sdk/pkg/field"

var (
	SendGridApiKeyField = field.StringField(
		"sendgrid-api-key",
		field.WithRequired(true),
		field.WithDescription("API key for SendGrid service."),
		field.WithIsSecret(true),
		field.WithDisplayName("API Key"),
		field.WithPlaceholder("SG.xxxxxxxxxxxxxxxxxxxx"),
	)

	SendGridRegionField = field.StringField(
		"sendgrid-region",
		field.WithRequired(false),
		field.WithDefaultValue("global"),
		field.WithDescription("Region for SendGrid service ex: global or eu."),
		field.WithDisplayName("Region"),
		field.WithPlaceholder("global"),
	)

	IgnoreSubusers = field.BoolField(
		"ignore-subusers",
		field.WithDefaultValue(false),
		field.WithDescription("Ignore subusers in the SendGrid account, subusers are an upgraded feature of sendgrid."),
		field.WithDisplayName("Ignore Subusers"),
	)

	BaseURLField = field.StringField(
		"base-url",
		field.WithDescription("Override the SendGrid API URL (for testing)"),
		field.WithHidden(true),
		field.WithExportTarget(field.ExportTargetCLIOnly),
	)
)

//go:generate go run ./gen
var Config = field.NewConfiguration(
	[]field.SchemaField{
		SendGridApiKeyField,
		SendGridRegionField,
		IgnoreSubusers,
		BaseURLField,
	},
	field.WithConnectorDisplayName("SendGrid"),
	field.WithIconUrl("/static/app-icons/sendgrid.svg"),
	field.WithHelpUrl("/docs/baton/sendgrid"),
)
