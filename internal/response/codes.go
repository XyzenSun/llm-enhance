package response

const (
	CodeSuccess                  = 0
	CodeBadRequest               = 1001
	CodeNotFound                 = 1002
	CodeUnsupportedProxyParam    = 1003
	CodeRequestFormatInvalid     = 1004
	CodeAuthFailed               = 2001
	CodeChannelDisabled          = 2002
	CodeChannelNotFound          = 2003
	CodeRuleProcessingFailed     = 3001
	CodeStreamingConflict        = 3002
	CodeBodyProcessingFailed     = 3003
	CodeUpstreamTimeout          = 4001
	CodeUpstreamFailure          = 4002
	CodeFakeStreamFailed         = 4003
	CodeUpstreamResponseTooLarge = 4004
	CodeClientException          = 5001
	CodeClientTimeout            = 5002
	CodeInternalError            = 5003
	CodeConfigError              = 5004
)

type CodeInfo struct {
	HTTPStatus int
	Message    string
}

var codeTable = map[int]CodeInfo{
	CodeSuccess:                  {200, "成功"},
	CodeBadRequest:               {400, "request parameter error"},
	CodeNotFound:                 {404, "route not found"},
	CodeUnsupportedProxyParam:    {400, "unsupported proxy routing parameter"},
	CodeRequestFormatInvalid:     {400, "request format is incompatible"},
	CodeAuthFailed:               {401, "authentication failed"},
	CodeChannelDisabled:          {403, "channel is disabled"},
	CodeChannelNotFound:          {404, "channel does not exist"},
	CodeRuleProcessingFailed:     {400, "rule matching or request processing failed"},
	CodeStreamingConflict:        {400, "streaming request conflicts with execution strategy"},
	CodeBodyProcessingFailed:     {400, "semantic request body processing failed"},
	CodeUpstreamTimeout:          {504, "upstream timeout"},
	CodeUpstreamFailure:          {502, "upstream connection failure"},
	CodeFakeStreamFailed:         {502, "nonstream-to-stream processing failed"},
	CodeUpstreamResponseTooLarge: {502, "upstream response exceeds allowed buffer size"},
	CodeClientException:          {500, "client connection exception"},
	CodeClientTimeout:            {408, "client connection timeout"},
	CodeInternalError:            {500, "internal server error"},
	CodeConfigError:              {500, "config parse or validation error"},
}

func Info(code int) CodeInfo {
	if info, ok := codeTable[code]; ok {
		return info
	}
	return codeTable[CodeInternalError]
}
