package a2a_test

import (
	"testing"

	"github.com/f1xgun/onevoice/pkg/a2a"
	"github.com/stretchr/testify/assert"
)

// Compile-time check that NATSTransport implements Transport.
var _ a2a.Transport = (*a2a.NATSTransport)(nil)

func TestNATSTransport_ImplementsTransport(t *testing.T) {
	assert.NotNil(t, new(a2a.NATSTransport))
}
