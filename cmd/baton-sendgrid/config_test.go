package main

import (
	"testing"

	"github.com/conductorone/baton-sdk/pkg/test"
	"github.com/conductorone/baton-sendgrid/pkg/config"
)

func TestConfigs(t *testing.T) {
	testCases := []test.TestCase{
		// Add test cases here.
	}

	test.ExerciseTestCases(t, config.Config, nil, testCases)
}
