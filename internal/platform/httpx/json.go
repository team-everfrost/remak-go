package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const maxJSONBodyBytes = 1 << 20

type envelope struct {
	Message string `json:"message"`
	Data    any    `json:"data"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		if errors.Is(err, io.EOF) {
			return BadRequest("empty_body", "요청 본문이 필요합니다")
		}
		return BadRequest("invalid_json", "JSON 요청 형식이 올바르지 않습니다")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return BadRequest("multiple_json_values", "JSON 값은 하나만 전달할 수 있습니다")
	}
	return nil
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(envelope{Message: "success", Data: data})
}

func WriteError(w http.ResponseWriter, err error) {
	appErr := AsError(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(appErr.Status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{Error: errorBody{
		Code:    appErr.Code,
		Message: appErr.Message,
	}})
}
