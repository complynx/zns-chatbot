package avatar_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/avatar"
)

func TestTransformDefaultsCanonicalAnglesAndBounds(t *testing.T) {
	t.Parallel()
	identity, err := avatar.NormalizeTransform(nil)
	require.NoError(t, err)
	require.Equal(t, avatar.IdentityTransform(), identity)
	value := avatar.Transform{ZoomPPM: avatar.FramePPM, AngleMilli: avatar.HalfTurnMilli}
	canonical, err := avatar.NormalizeTransform(&value)
	require.NoError(t, err)
	require.Equal(t, -avatar.HalfTurnMilli, canonical.AngleMilli)
	require.Equal(t, avatar.HalfTurnMilli, value.AngleMilli)
	for _, value := range []avatar.Transform{
		{}, {ZoomPPM: avatar.MinZoomPPM - 1}, {ZoomPPM: avatar.MaxZoomPPM + 1},
		{ZoomPPM: avatar.FramePPM, OffsetXPPM: avatar.MaxOffsetPPM + 1},
		{ZoomPPM: avatar.FramePPM, OffsetYPPM: -avatar.MaxOffsetPPM - 1},
		{ZoomPPM: avatar.FramePPM, AngleMilli: avatar.HalfTurnMilli + 1},
	} {
		_, err = avatar.NormalizeTransform(&value)
		require.ErrorIs(t, err, avatar.ErrInvalidTransform)
	}
	for _, value := range []avatar.Transform{
		{ZoomPPM: avatar.MinZoomPPM, OffsetXPPM: avatar.MaxOffsetPPM, AngleMilli: -avatar.HalfTurnMilli},
		{ZoomPPM: avatar.MaxZoomPPM, OffsetYPPM: -avatar.MaxOffsetPPM},
	} {
		_, err = avatar.NormalizeTransform(&value)
		require.NoError(t, err)
	}
}

func TestRenderInputPreservesRawAndParsesStrictBoundedHeader(t *testing.T) {
	t.Parallel()
	raw := []byte{0x89, 'P', 'N', 'G', 13, 10, 26, 10}
	source, transform, err := avatar.ReadInput(bytes.NewReader(raw))
	require.NoError(t, err)
	require.Equal(t, raw, source)
	require.Equal(t, avatar.IdentityTransform(), transform)
	expected := avatar.Transform{
		ZoomPPM:    2 * avatar.FramePPM,
		OffsetXPPM: -avatar.FramePPM / 4,
		AngleMilli: avatar.HalfTurnMilli,
	}
	header, err := json.Marshal(map[string]any{"protocol_version": 2, "transform": expected})
	require.NoError(t, err)
	protocol := append(append(header, '\n'), raw...)
	source, transform, err = avatar.ReadInput(bytes.NewReader(protocol))
	require.NoError(t, err)
	expected.AngleMilli = -avatar.HalfTurnMilli
	require.Equal(t, expected, transform)
	require.Equal(t, raw, source)
	for _, input := range []string{
		"{\"protocol_version\":3,\"transform\":{\"zoom_ppm\":1000000}}\nimage",
		"{\"protocol_version\":2,\"transform\":{\"zoom_ppm\":1000000},\"unknown\":1}\nimage",
		"{\"protocol_version\":2,\"transform\":{\"zoom_ppm\":1000000,\"unknown\":1}}\nimage",
		"{\"protocol_version\":2,\"transform\":{\"zoom_ppm\":0}}\nimage",
		"{\"protocol_version\":2,\"transform\":null}\nimage",
		"{} {}\nimage", `{}`,
		"{" + strings.Repeat(" ", avatar.MaxHeaderBytes) + "}\nimage",
	} {
		_, _, err = avatar.ReadInput(strings.NewReader(input))
		require.Error(t, err)
	}
	_, _, err = avatar.ReadInput(bytes.NewReader(make([]byte, avatar.MaxInputBytes+1)))
	require.ErrorIs(t, err, avatar.ErrImageLimit)
}

func TestRenderedTransformEchoMustMatchVersionAndCanonicalRequest(t *testing.T) {
	t.Parallel()
	identity := avatar.IdentityTransform()
	require.True(t, (avatar.Rendered{ProcessorVersion: avatar.ProcessorVersion}).MatchesTransform(identity))
	edited := identity
	edited.OffsetXPPM = 1
	result := avatar.Rendered{ProcessorVersion: avatar.EditedProcessorVersion, Transform: &edited}
	require.True(t, result.MatchesTransform(edited))
	require.False(t, result.MatchesTransform(identity))
	other := edited
	other.OffsetXPPM++
	require.False(t, result.MatchesTransform(other))
	result.Transform = nil
	require.False(t, result.MatchesTransform(edited))
	result.Transform = &edited
	result.ProcessorVersion = avatar.ProcessorVersion
	require.False(t, result.MatchesTransform(edited))
	result.Transform = &identity
	require.False(t, result.MatchesTransform(identity))
}
