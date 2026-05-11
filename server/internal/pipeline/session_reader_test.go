package pipeline_test

import (
	"testing"

	"github.com/lx-wnk/agent-dashboard/server/internal/pipeline"
	"github.com/stretchr/testify/require"
)

func TestExtractJsonBlock_LastBlock(t *testing.T) {
	text := "prose\n```json\n{\"a\":1}\n```\nmore\n```json\n{\"b\":2}\n```"
	result := pipeline.ExtractJsonBlock(text)
	require.NotNil(t, result)
	require.Equal(t, float64(2), result["b"])
}

func TestExtractJsonBlock_NoBlock(t *testing.T) {
	require.Nil(t, pipeline.ExtractJsonBlock("no code blocks here"))
}

func TestExtractJsonBlock_InvalidJSON(t *testing.T) {
	require.Nil(t, pipeline.ExtractJsonBlock("```json\n{invalid}\n```"))
}
