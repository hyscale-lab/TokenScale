package model

type CliReq struct {
	UUID string   `json:"uuid"`
	CLI  string   `json:"cli"`
	Env  []string `json:"env"`
	Args []string `json:"args"`
}

type CliDel struct {
	UUID string `json:"uuid"`
}

type PreloadReq struct {
	ModelPath string `json:"model_path"`
	TPSize    int    `json:"tensor_parallel_size"`
}

type CreateFileReq struct {
	FilePath    string `json:"file_path"`
	FileContent string `json:"file_content"`
}
