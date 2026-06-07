package crypto_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/f1xgun/onevoice/pkg/crypto"
	"github.com/f1xgun/onevoice/pkg/crypto/kmsfake"
)

// Compile-time assertion: FakeKMSEncrypter satisfies KMSEncrypter.
var _ crypto.KMSEncrypter = (*kmsfake.FakeKMSEncrypter)(nil)

func TestWipe_Nil(t *testing.T) {
	assert.NotPanics(t, func() { crypto.Wipe(nil) })
}

func TestWipe_Empty(t *testing.T) {
	b := []byte{}
	assert.NotPanics(t, func() { crypto.Wipe(b) })
	assert.Equal(t, 0, len(b))
}

func TestWipe_SmallBuffer(t *testing.T) {
	b := []byte{1, 2, 3, 4}
	crypto.Wipe(b)
	assert.Equal(t, []byte{0, 0, 0, 0}, b)
}

func TestWipe_LargeBuffer(t *testing.T) {
	b := make([]byte, 1024*1024)
	for i := range b {
		b[i] = 0xFF
	}
	crypto.Wipe(b)
	for _, v := range b {
		assert.Equal(t, byte(0), v)
	}
}

func TestWipe_PreservesLength(t *testing.T) {
	b := make([]byte, 16)
	for i := range b {
		b[i] = 0xAB
	}
	origLen := len(b)
	origCap := cap(b)
	crypto.Wipe(b)
	assert.Equal(t, origLen, len(b))
	assert.Equal(t, origCap, cap(b))
}

func TestKMSEncrypter_InterfaceShape(t *testing.T) {
	t.Log("compile-time assertion at package level verifies FakeKMSEncrypter satisfies KMSEncrypter")
}
