package logging

// 外部模型调用日志消息（msg 字段），便于用中文关键字检索。
const (
	ExternalModelRequestStartMsg    = "【外部模型】请求开始"
	ExternalModelResponseOKMsg      = "【外部模型】响应成功"
	ExternalModelRequestFailedMsg   = "【外部模型】请求失败"
	ExternalModelResponseInvalidMsg = "【外部模型】响应无效"
)

// ExternalModelBodyMaxChars 限制单条外部模型日志字段长度，避免 prompt/正文撑爆日志文件。
const ExternalModelBodyMaxChars = 32000

// ExternalModelLogBody 截断超长日志正文，尾部追加 ...(truncated)。
func ExternalModelLogBody(value string) string {
	if ExternalModelBodyMaxChars <= 0 || len(value) <= ExternalModelBodyMaxChars {
		return value
	}
	return value[:ExternalModelBodyMaxChars] + "...(truncated)"
}
