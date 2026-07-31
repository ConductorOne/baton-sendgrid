![Baton Logo](./docs/images/baton-logo.png)

#

`baton-sendgrid` [![Go Reference](https://pkg.go.dev/badge/github.com/conductorone/baton-sendgrid.svg)](https://pkg.go.dev/github.com/conductorone/baton-sendgrid) ![ci](https://github.com/conductorone/baton-sendgrid/actions/workflows/ci.yaml/badge.svg) ![verify](https://github.com/conductorone/baton-sendgrid/actions/workflows/verify.yaml/badge.svg)

`baton-sendgrid` is a connector for built using the [Baton SDK](https://github.com/conductorone/baton-sdk).

Check out [Baton](https://github.com/conductorone/baton) to learn more the project in general.

# Getting Started

## Prerequisites

You need to pass the sendgrid-api-key:

1. Create a Sendgrid app.
2. Create an API KEY https://app.sendgrid.com/settings/api_keys.
3. Run it.

Obs: if you have a basic account, you can ignore the subusers using ```.

## brew

```
brew install conductorone/baton/baton conductorone/baton/baton-sendgrid
baton-sendgrid
baton resources
```

## docker

```
docker run --rm -v $(pwd):/out -e BATON_DOMAIN_URL=domain_url -e BATON_API_KEY=apiKey -e BATON_USERNAME=username ghcr.io/conductorone/baton-sendgrid:latest -f "/out/sync.c1z"
docker run --rm -v $(pwd):/out ghcr.io/conductorone/baton:latest -f "/out/sync.c1z" resources
```

## source

```
go install github.com/conductorone/baton/cmd/baton@main
go install github.com/conductorone/baton-sendgrid/cmd/baton-sendgrid@main

baton-sendgrid

baton resources
```

# Data Model

`baton-sendgrid` will pull down information about the following resources:

- Teammates
- Teammate Invitations (pending invitations)
- Subusers (if not using `--ignore-subusers`)
- Scopes

## Teammates in sub-accounts

A SendGrid sub-account (subuser) can have its own teammates that were provisioned directly
inside that sub-account and never granted access at the parent-account level. These
teammates are invisible to the parent account's teammates list.

Unless `--ignore-subusers` is set, `baton-sendgrid` also looks inside each sub-account
(via the SendGrid `on-behalf-of` header) for teammates that only exist there. They're synced
as regular `teammate` resources — same ID scheme as any other teammate (`username`) — with
their sub-account set as the parent resource, so access reviews reflect the humans who
actually have access, not just the sub-accounts themselves. A teammate who already has
parent-level access is not duplicated just because they can also reach one or more
sub-accounts.

# Provisioning & Deprovisioning

`baton-sendgrid` supports provisioning and deprovisioning capabilities when the `--provisioning` flag is enabled:

## Teammate Invitations
- **Provision (Create)**: Invite new teammates by email with optional scopes and admin permissions
  - `is_admin`: Whether the teammate has admin privileges (default: `false`)
  - `scopes`: List of permission scopes for non-admin teammates (default: `["user.profile.read"]`)
  - Note: Scopes are ignored when `is_admin` is `true` (admins have full access)
- **Deprovision (Delete)**: Revoke pending teammate invitations

## Teammates
- **Deprovision (Delete)**: Remove existing teammates from the SendGrid account

To enable provisioning capabilities, run the connector with the `-p` or `--provisioning` flag:

```bash
baton-sendgrid --sendgrid-api-key=<your-api-key> --provisioning
```

# Contributing, Support and Issues

We started Baton because we were tired of taking screenshots and manually
building spreadsheets. We welcome contributions, and ideas, no matter how
small&mdash;our goal is to make identity and permissions sprawl less painful for
everyone. If you have questions, problems, or ideas: Please open a GitHub Issue!

See [CONTRIBUTING.md](https://github.com/ConductorOne/baton/blob/main/CONTRIBUTING.md) for more details.

# `baton-sendgrid` Command Line Usage

```
baton-sendgrid

Usage:
  baton-sendgrid [flags]
  baton-sendgrid [command]

Available Commands:
  capabilities       Get connector capabilities
  completion         Generate the autocompletion script for the specified shell
  help               Help about any command

Flags:
      --client-id string          The client ID used to authenticate with ConductorOne ($BATON_CLIENT_ID)
      --client-secret string      The client secret used to authenticate with ConductorOne ($BATON_CLIENT_SECRET)
  -f, --file string               The path to the c1z file to sync with ($BATON_FILE) (default "sync.c1z")
  -h, --help                      help for baton-sendgrid
      --ignore-subusers           Ignore subusers in the SendGrid account, subusers are an upgraded feature of sendgrid. ($BATON_IGNORE_SUBUSERS)
      --log-format string         The output format for logs: json, console ($BATON_LOG_FORMAT) (default "json")
      --log-level string          The log level: debug, info, warn, error ($BATON_LOG_LEVEL) (default "info")
  -p, --provisioning              This must be set in order for provisioning actions to be enabled ($BATON_PROVISIONING)
      --sendgrid-api-key string   required: API key for SendGrid service. ($BATON_SENDGRID_API_KEY)
      --sendgrid-region string    Region for SendGrid service ex: global or eu. ($BATON_SENDGRID_REGION) (default "global")
      --skip-full-sync            This must be set to skip a full sync ($BATON_SKIP_FULL_SYNC)
      --ticketing                 This must be set to enable ticketing support ($BATON_TICKETING)
  -v, --version                   version for baton-sendgrid

Use "baton-sendgrid [command] --help" for more information about a command.
```
