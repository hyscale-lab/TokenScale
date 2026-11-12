package model

type RouterRequest struct {
	ReqID            string
	IsPrefill        bool
	CapacityExceed   bool
	NumPrefillTokens int
}

type RouterReply struct {
	InstanceUUID string
}

type MinTokNumRequest struct {
}

type MinTokNumReply struct {
	NumTok       int64
	InstanceUUID string
}
