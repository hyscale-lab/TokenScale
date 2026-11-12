package model

type ScalerRequest struct {
	RequestType    string `json:"request_type"`
	PromptLength   int    `json:"prompt_length"`
	GtDecodeLength int    `json:"ground_truth_decode_length"`
}
