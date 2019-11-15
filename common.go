package main

import (
	"errors"
	"strconv"
	"strings"
)

const (
	NOTIFY_TRANSFER = "transfer"

	ONT_CONTRACT_ADDRESS               = "0100000000000000000000000000000000000000"
	ONT_CONTRACT_ADDRESS_BASE58        = "AFmseVrdL9f9oyCzZefL9tG6UbvhUMqNMV"
	ONG_CONTRACT_ADDRESS               = "0200000000000000000000000000000000000000"
	ONG_CONTRACT_ADDRESS_BASE58        = "AFmseVrdL9f9oyCzZefL9tG6UbvhfRZMHJ"
	GOVERNANCE_CONTRACT_ADDRESS        = "0700000000000000000000000000000000000000"
	GOVERNANCE_CONTRACT_ADDRESS_BASE58 = "AFmseVrdL9f9oyCzZefL9tG6UbviEH9ugK"
)

const (
	HEARTBEAT_MODULE = "holder"

	DEFAULT_HEARTBEAT_TIMEOUT_TIME    = 60 //s
	DEFAULT_HAARTBEAT_UPDATE_INTERVAL = 10 //s

	DEFAULT_UPDATE_SYNCED_BLOCK_HEIGHT_INTERVAL = 5 //s
	DEFAULT_UPDATE_ASSET_HOLDER_COUNT_INTERVAL  = 2 //s
)

type Heartbeat struct {
	Module     string
	UpdateTime string
	NodeId     uint32
}

type TxTransfer struct {
	TxHash   string
	Contract string
	From     string
	To       string
	Amount   uint64
}

type TxEventNotify struct {
	TxHash      string
	Height      uint32
	State       int
	GasConsumed int
	Notify      string
}

type AssetHolder struct {
	Address  string
	Contract string
	Balance  uint64
}

type HttpServerRequest struct {
	Method string
	Qid    string
	Params map[string]string
}

func (this *HttpServerRequest) GetParamString(param string) (string, error) {
	val, ok := this.Params[strings.ToLower(param)]
	if !ok {
		return "", ERR_PARAM_NOT_EXIST
	}
	return val, nil
}

func (this *HttpServerRequest) GetParamInt(param string) (int, error) {
	val, ok := this.Params[strings.ToLower(param)]
	if !ok {
		return 0, ERR_PARAM_NOT_EXIST
	}
	intVal, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, err
	}
	return int(intVal), nil
}

var (
	ERR_PARAM_NOT_EXIST = errors.New("param does not exist")
)

type HttpServerResponse struct {
	Qid       string      `json:"qid"`
	Method    string      `json:"method"`
	ErrorCode uint32      `json:"error_code"`
	ErrorInfo string      `json:"error_info"`
	Result    interface{} `json:"result"`
	Total     interface{} `json:"total,omitempty"`
}

const (
	ERR_SUCCESS        = 0
	ERR_INVALID_PARAMS = 1001
	ERR_INVALID_METHOD = 1002
	ERR_INTERNAL       = 9999
)

var HttpServerErrorDesc = map[uint32]string{
	ERR_SUCCESS:        "",
	ERR_INVALID_PARAMS: "invalid params",
	ERR_INVALID_METHOD: "invalid method",
	ERR_INTERNAL:       "internal error",
}

func GetHttpServerErrorDesc(errorCode uint32) string {
	desc, ok := HttpServerErrorDesc[errorCode]
	if !ok {
		return HttpServerErrorDesc[ERR_INTERNAL]
	}
	return desc
}
