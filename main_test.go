package main

import (
	"testing"

	"github.com/kubernetes-csi/csi-test/v4/pkg/sanity"
)

// TestCivoCSI runs the Sanity test suite
func TestCivoCSI(t *testing.T) {
	// Setup the full driver and its environment
	// ... setup driver ...
	config := sanity.NewTestConfig()

	// Set configuration options as needed
	// cfg.Address = endpoint

	// Now call the test suite
	sanity.Test(t, config)
}
