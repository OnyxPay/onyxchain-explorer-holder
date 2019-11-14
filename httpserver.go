package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	log4 "github.com/alecthomas/log4go"
)

var DefHttpSvr = NewHttpServer()

func init() {
	DefHttpSvr.RegHandler("getAssetInfo", DefHttpSvr.GetAssetInfo)
	DefHttpSvr.RegHandler("getAssetHolderCount", DefHttpSvr.GetAssetHolderCount)
	DefHttpSvr.RegHandler("getAssetHolder", DefHttpSvr.GetAssetHolder)
	DefHttpSvr.RegHandler("getBalance", DefHttpSvr.GetBalance)
}

type HttpServer struct {
	port       uint
	httpSvr    *http.Server
	httpSvtMux *http.ServeMux
	handlers   map[string]func(req *HttpServerRequest, resp *HttpServerResponse)
}

func NewHttpServer() *HttpServer {
	return &HttpServer{
		handlers: make(map[string]func(req *HttpServerRequest, resp *HttpServerResponse)),
	}
}

func (this *HttpServer) Start(port uint) {
	this.port = port
	this.httpSvtMux = http.NewServeMux()
	this.httpSvr = &http.Server{
		Addr:    fmt.Sprintf("0.0.0.0:%d", port),
		Handler: this.httpSvtMux,
	}
	this.httpSvtMux.HandleFunc("/", this.Handler)
	go func() {
		err := this.httpSvr.ListenAndServe()
		if err != nil {
			panic(err)
		}
	}()
}

func (this *HttpServer) RegHandler(method string, handler func(request *HttpServerRequest, response *HttpServerResponse)) {
	this.handlers[strings.ToLower(method)] = handler
}

type AssetHolderPer struct {
	Address string  `json:"address"`
	Balance uint64  `json:"balance"`
	Percent float64 `json:"percent"`
}

func (this *HttpServer) Handler(w http.ResponseWriter, r *http.Request) {
	resp := &HttpServerResponse{}
	defer func() {
		w.Header().Add("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("content-type", "application/json;charset=utf-8")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusOK)

		if resp.ErrorInfo == "" {
			resp.ErrorInfo = GetHttpServerErrorDesc(resp.ErrorCode)
		}
		data, err := json.Marshal(resp)
		if err != nil {
			log4.Error("HttpServer json.Marshal HttpServerResponse:%+v error:%s", resp, err)
			return
		}
		_, err = w.Write(data)
		if err != nil {
			log4.Error("HttpServer Write error:%s", err)
			return
		}
	}()

	method := strings.TrimLeft(r.URL.Path, "/")

	if method == "favicon.ico" {
		return
	}

	querys := r.URL.Query()
	params := make(map[string]string)
	qid := ""
	for k, vs := range querys {
		if k == "qid" {
			qid = vs[0]
			continue
		}
		params[strings.ToLower(k)] = vs[0]
	}
	resp.Qid = qid
	resp.Method = method
	resp.ErrorCode = ERR_SUCCESS

	req := &HttpServerRequest{
		Method: method,
		Qid:    qid,
		Params: params,
	}

	handler, ok := this.handlers[strings.ToLower(method)]
	if !ok {
		resp.ErrorCode = ERR_INVALID_METHOD
		return
	}

	log4.Info("[HttpServerRequest]:%+v", req)
	handler(req, resp)
}

type AssetInfo struct {
	Symbol      string `json:"symbol"`
	TotalSupply uint64 `json:"total_supply"`
	Precision   byte   `json:"precision"`
}

func (this *HttpServer) GetAssetInfo(req *HttpServerRequest, resp *HttpServerResponse) {
	contract, err := req.GetParamString("contract")
	if err != nil {
		resp.ErrorCode = ERR_INVALID_PARAMS
		log4.Info("GetAssetHolder GetParamString contract error:%s", err)
		return
	}
	if contract != ONG_CONTRACT_ADDRESS && contract != ONT_CONTRACT_ADDRESS {
		resp.ErrorCode = ERR_INVALID_PARAMS
		return
	}

	assetInfo, err := this.getAssetInfo(contract)
	if err != nil {
		log4.Info("GetAssetInfo error:%s", err)
		resp.ErrorCode = ERR_INTERNAL
		return
	}
	resp.Result = assetInfo
}

func (this *HttpServer) getAssetInfo(contract string) (*AssetInfo, error) {
	var err error
	assetInfo := &AssetInfo{}
	switch contract {
	case ONT_CONTRACT_ADDRESS:
		assetInfo.TotalSupply, err = DefOntologyMgr.GetOntSdk().Native.Ont.TotalSupply()
		if err != nil {
			return nil, err
		}
		assetInfo.Symbol, err = DefOntologyMgr.GetOntSdk().Native.Ont.Symbol()
		if err != nil {
			return nil, err
		}
		assetInfo.Precision, err = DefOntologyMgr.GetOntSdk().Native.Ont.Decimals()
		if err != nil {
			return nil, err
		}
	case ONG_CONTRACT_ADDRESS:
		assetInfo.TotalSupply, err = DefOntologyMgr.GetOntSdk().Native.Ong.TotalSupply()
		if err != nil {
			return nil, err
		}
		assetInfo.Symbol, err = DefOntologyMgr.GetOntSdk().Native.Ong.Symbol()
		if err != nil {
			return nil, err
		}
		assetInfo.Precision, err = DefOntologyMgr.GetOntSdk().Native.Ong.Decimals()
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unknown contract")
	}
	return assetInfo, nil
}

func (this *HttpServer) GetAssetHolderCount(req *HttpServerRequest, resp *HttpServerResponse) {
	contract, err := req.GetParamString("contract")
	if err != nil {
		resp.ErrorCode = ERR_INVALID_PARAMS
		log4.Info("GetAssetHolder GetParamString contract error:%s", err)
		return
	}
	if contract != ONG_CONTRACT_ADDRESS && contract != ONT_CONTRACT_ADDRESS {
		resp.ErrorCode = ERR_INVALID_PARAMS
		return
	}
	resp.Result = DefOntologyMgr.GetAssetHolderCount(contract)
}

func (this *HttpServer) GetAssetHolder(req *HttpServerRequest, resp *HttpServerResponse) {
	from, err := req.GetParamInt("from")
	if err != nil {
		resp.ErrorCode = ERR_INVALID_PARAMS
		log4.Info("GetAssetHolder GetParamInt from error:%s", err)
		return
	}
	count, err := req.GetParamInt("count")
	if err != nil {
		resp.ErrorCode = ERR_INVALID_PARAMS
		log4.Info("GetAssetHolder GetParamInt count error:%s", err)
		return
	}
	contract, err := req.GetParamString("contract")
	if err != nil {
		resp.ErrorCode = ERR_INVALID_PARAMS
		log4.Info("GetAssetHolder GetParamString contract error:%s", err)
		return
	}

	if from < 0 || count < 0 ||
		(contract != ONG_CONTRACT_ADDRESS && contract != ONT_CONTRACT_ADDRESS) {
		resp.ErrorCode = ERR_INVALID_PARAMS
		return
	}

	if count > int(DefConfig.MaxQueryPageSize) {
		resp.ErrorCode = ERR_INVALID_PARAMS
		resp.ErrorInfo = fmt.Sprintf("count out of range[1, %d]", DefConfig.MaxQueryPageSize)
		return
	}

	totalSupply := uint64(1)
	switch contract {
	case ONT_CONTRACT_ADDRESS:
		totalSupply = DefOntologyMgr.ONTTotalSupply
	case ONG_CONTRACT_ADDRESS:
		totalSupply = DefOntologyMgr.ONGTotalSupply
	}
	if totalSupply == 0 {
		resp.ErrorCode = ERR_INTERNAL
		return
	}

	assetHolders, err := DefOntologyMgr.GetAssetHolder(from, count, "", contract)
	if err != nil {
		resp.ErrorCode = ERR_INTERNAL
		log4.Info("GetAssetHolder GetAssetHolder error:%s", err)
		return
	}

	assetHolderPers := make([]*AssetHolderPer, 0, len(assetHolders))
	for _, assetHolder := range assetHolders {
		assetHolderPer := &AssetHolderPer{
			Address: assetHolder.Address,
			Balance: assetHolder.Balance,
			Percent: float64(assetHolder.Balance) / float64(totalSupply),
		}
		assetHolderPers = append(assetHolderPers, assetHolderPer)
	}

	resp.Result = assetHolderPers

	resp.Total = DefOntologyMgr.GetAssetHolderCount(contract)
}

type AssetBalance struct {
	Contract string `json:"contract"`
	Balance  uint64 `json:"balance"`
}

func (this *HttpServer) GetBalance(req *HttpServerRequest, resp *HttpServerResponse) {
	address, err := req.GetParamString("address")
	if err != nil {
		resp.ErrorCode = ERR_INVALID_PARAMS
		log4.Info("GetAssetHolder GetParamString address error:%s", err)
		return
	}
	contract, err := req.GetParamString("contract")
	if err != nil {
		if err != ERR_PARAM_NOT_EXIST {
			resp.ErrorCode = ERR_INVALID_PARAMS
			log4.Info("GetAssetHolder GetParamString contract error:%s", err)
			return
		}
	}
	assetHolders, err := DefOntologyMgr.GetAssetHolder(0, 0, address, contract)
	if err != nil {
		resp.ErrorCode = ERR_INTERNAL
		log4.Info("GetBalance GetAssetHolder address:%s contract:%s error:%s", address, contract, err)
		return
	}

	assetBalances := make([]*AssetBalance, 0, len(assetHolders))
	for _, assetHolder := range assetHolders {
		assetBalances = append(assetBalances,
			&AssetBalance{
				Contract: assetHolder.Contract,
				Balance:  assetHolder.Balance,
			})
	}

	resp.Result = assetBalances
}
