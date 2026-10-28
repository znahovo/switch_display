package dto

// LogEntryDTO 是一条日志。
type LogEntryDTO struct {
	// Seq 是单调递增序号，前端按它做增量拉取。
	Seq uint64 `json:"seq"`
	// Time 是写入时间，格式 `2006-01-02T15:04:05.000`（本地时间，不带时区）。
	//
	// 不带时区的原因：日志是给人看的，用户只关心自己机器上的钟点；
	// 时区信息在日志页上是噪音。
	Time string `json:"time"`
	// Level 是 debug/info/warn/error/raw。
	Level string `json:"level"`
	// Source 是来源（app / deskflow / …）。
	Source string `json:"source"`
	// Message 是正文。
	Message string `json:"message"`
	// Fields 是 kv 渲染结果，形如 `pid=123 mode=server`。
	Fields string `json:"fields,omitempty"`
}

// LogBatchDTO 是一次增量查询的结果。
type LogBatchDTO struct {
	// Entries 是本次返回的条目（按序号升序）。
	Entries []LogEntryDTO `json:"entries"`
	// Latest 是缓冲当前的最大序号；前端保存它作为下次的游标。
	Latest uint64 `json:"latest"`
	// Oldest 是缓冲里还剩的最小序号（空缓冲为 0）。
	Oldest uint64 `json:"oldest"`
	// Dropped 是因容量上限被丢弃的累计条数。
	Dropped int `json:"dropped"`
	// Capacity 是缓冲容量（拿去算「还剩多少」）。
	Capacity int `json:"capacity"`
	// FilePath 是日志落盘路径；空表示不落盘。
	FilePath string `json:"filePath"`
	// FileError 是落盘错误说明；空表示正常。
	FileError string `json:"fileError,omitempty"`
	// Lost 表示本次返回与前端上次游标之间已有缺失。
	Lost bool `json:"lost,omitempty"`
}
