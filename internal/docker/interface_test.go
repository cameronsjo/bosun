package docker

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestableClient(t *testing.T) {
	mock := NewMockDockerAPI()
	tc := NewTestableClient(mock)

	require.NotNil(t, tc)
	assert.Equal(t, mock, tc.api)
}

func TestTestableClient_API(t *testing.T) {
	mock := NewMockDockerAPI()
	tc := NewTestableClient(mock)

	got := tc.API()
	assert.Equal(t, mock, got)
}

func TestTestableClient_Close(t *testing.T) {
	t.Run("delegates to api", func(t *testing.T) {
		mock := NewMockDockerAPI()
		tc := NewTestableClient(mock)

		err := tc.Close()
		require.NoError(t, err)
		assert.Equal(t, 1, mock.CloseCalls)
	})

	t.Run("nil api returns nil", func(t *testing.T) {
		tc := &TestableClient{api: nil}
		err := tc.Close()
		require.NoError(t, err)
	})

	t.Run("propagates error", func(t *testing.T) {
		mock := NewMockDockerAPI()
		mock.CloseFunc = func() error {
			return errors.New("close failed")
		}
		tc := NewTestableClient(mock)

		err := tc.Close()
		require.Error(t, err)
		assert.Equal(t, "close failed", err.Error())
	})
}

func TestClient_Raw(t *testing.T) {
	t.Run("nil when created via NewClientWithAPI", func(t *testing.T) {
		mock := NewMockDockerAPI()
		client := NewClientWithAPI(mock)
		assert.Nil(t, client.Raw())
	})
}
