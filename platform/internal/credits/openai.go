package credits

import "encoding/json"

// ResponsesUsage reads only documented counters. Reasoning is an output subset;
// cached tokens are an input subset. Absent fields never become zero.
func ResponsesUsage(data []byte) Usage {
	var response struct {
		ID          string `json:"id"`
		Model       string `json:"model"`
		ServiceTier string `json:"service_tier"`
		Usage       *struct {
			Input        *int64 `json:"input_tokens"`
			Output       *int64 `json:"output_tokens"`
			InputDetails *struct {
				Cached     *int64 `json:"cached_tokens"`
				CacheWrite *int64 `json:"cache_write_tokens"`
			} `json:"input_tokens_details"`
			OutputDetails *struct {
				Reasoning *int64 `json:"reasoning_tokens"`
			} `json:"output_tokens_details"`
		} `json:"usage"`
	}
	unknown := Usage{Basis: basisUnknown}
	if len(data) > 1<<20 || json.Unmarshal(data, &response) != nil {
		return unknown
	}
	result := Usage{
		ResponseID:  response.ID,
		Model:       response.Model,
		ServiceTier: response.ServiceTier,
		Basis:       basisUnknown,
	}
	if response.Usage != nil {
		result.Basis = basisReported
		result.Input = response.Usage.Input
		result.Output = response.Usage.Output
		if response.Usage.InputDetails != nil {
			result.Cached = response.Usage.InputDetails.Cached
			result.CacheWrite = response.Usage.InputDetails.CacheWrite
		}
		if response.Usage.OutputDetails != nil {
			result.Reasoning = response.Usage.OutputDetails.Reasoning
		}
	}
	if result.Validate() != nil {
		return unknown
	}
	return result
}
