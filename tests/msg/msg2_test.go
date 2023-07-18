package msg

import (
	"github.com/ilius/is/v2"
	"testing"
)

func Test_tRun_EqualWithMsg(t *testing.T) {
	a := "test"
	b := "test"
	t.Run("test1", func(t *testing.T) {
		require.Equal(t, a, b, "fails: %v != %v", a, b)
	})
	t.Run("test2", func(t *testing.T) {
		require.Equal(t, a, b, 1234, true, 1.2)
	})
}
