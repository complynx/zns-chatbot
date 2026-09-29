package credits

import "encoding/json"

// TranscriptionUsage preserves token or duration billing dimensions exactly.
// The transcript and other response content are never copied to accounting.
func TranscriptionUsage(data []byte, model string) Usage {
	unknown := Usage{Basis: basisUnknown, Model: model}
	var response struct {
		Usage *struct {
			Type    string      `json:"type"`
			Seconds json.Number `json:"seconds"`
			Input   *int64      `json:"input_tokens"`
			Output  *int64      `json:"output_tokens"`
			Details *struct {
				Audio *int64 `json:"audio_tokens"`
				Text  *int64 `json:"text_tokens"`
			} `json:"input_token_details"`
		} `json:"usage"`
	}
	if len(data) > 1<<20 || json.Unmarshal(data, &response) != nil || response.Usage == nil {
		return unknown
	}
	usage := Usage{Basis: basisReported, Model: model}
	switch response.Usage.Type {
	case "tokens":
		usage.Input, usage.Output = response.Usage.Input, response.Usage.Output
		if response.Usage.Details != nil {
			usage.AudioInput, usage.TextInput = response.Usage.Details.Audio, response.Usage.Details.Text
		}
	case "duration":
		if response.Usage.Seconds == "" {
			return unknown
		}
		seconds := string(response.Usage.Seconds)
		usage.AudioSeconds = &seconds
	default:
		return unknown
	}
	if usage.Validate() != nil {
		return unknown
	}
	return usage
}
