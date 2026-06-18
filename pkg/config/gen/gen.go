package main

import (
	cfg "github.com/conductorone/baton-sendgrid/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/config"
)

func main() { config.Generate("sendgrid", cfg.Config) }
