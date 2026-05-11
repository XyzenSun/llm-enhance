package response

import (
	"encoding/json"
	"net/http"
	"time"
)

type Envelope struct {
	Code      int    `json:"code"`
	Timestamp string `json:"timestamp"`
	Data      any    `json:"data"`
	Message   string `json:"message"`
}

func New(code int, data any, msg string) Envelope {
	if msg == "" {
		msg = Info(code).Message
	}
	return Envelope{Code: code, Timestamp: time.Now().Format("2006-01-02 15:04:05"), Data: data, Message: msg}
}

func WriteJSON(w http.ResponseWriter, status int, code int, data any, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(New(code, data, msg))
}

func WriteError(w http.ResponseWriter, code int, msg string) {
	info := Info(code)
	WriteJSON(w, info.HTTPStatus, code, nil, msg)
}

func WriteSuccess(w http.ResponseWriter, data any) {
	WriteJSON(w, http.StatusOK, CodeSuccess, data, "")
}
