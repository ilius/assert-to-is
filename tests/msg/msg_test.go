package msg

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEqualWithMsg(t *testing.T) {
	a := "test"
	b := "test"
	require.Equal(t, a, b, "fails: %v != %v", a, b)

	require.Equal(t, a, b, 1234, true, 1.2)
}
