package common

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
)

func TestTaskPluginChannelHasNoOrdinaryAPIType(t *testing.T) {
	apiType, ok := ChannelType2APIType(constant.ChannelTypeTaskPlugin)
	assert.Equal(t, -1, apiType)
	assert.False(t, ok)
}

func TestQwen3GuardChannelRemainsSeparateFromTaskPlugins(t *testing.T) {
	apiType, ok := ChannelType2APIType(constant.ChannelTypeQwen3Guard)
	assert.True(t, ok)
	assert.Equal(t, constant.APITypeQwen3Guard, apiType)
	assert.Equal(t, "http://localhost:11434", constant.GetChannelBaseURL(constant.ChannelTypeQwen3Guard))
	assert.Empty(t, constant.GetChannelBaseURL(constant.ChannelTypeTaskPlugin))
}
