package main

import (
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	log4 "github.com/alecthomas/log4go"
	ontsdk "github.com/ontio/ontology-go-sdk"
	sdkcom "github.com/ontio/ontology-go-sdk/common"
)

var DefOntologyMgr *OntologyManager

const (
	SYNC_EVTNOTIFY_CHAN_SIZE = 1000
)

type EventNotify struct {
	BlockHeight   uint32
	EventNotifies []*sdkcom.SmartContactEvent
}

type OntologyManager struct {
	ontSdk                     *ontsdk.OntologySdk
	mysqlHelper                *MySqlHelper
	syncedEvtNotifyBlockHeight uint32
	syncEvtNotifyChan          chan *EventNotify
	ONTTotalSupply             uint64
	ONGTotalSupply             uint64
	hb                         *Heartbeat
	holderCounts               map[string]int
	exitCh                     chan interface{}
	lock                       sync.RWMutex
}

func NewOntologyManager(ontSdk *ontsdk.OntologySdk, mySqlHelper *MySqlHelper) *OntologyManager {
	return &OntologyManager{
		ontSdk:            ontSdk,
		mysqlHelper:       mySqlHelper,
		syncEvtNotifyChan: make(chan *EventNotify, SYNC_EVTNOTIFY_CHAN_SIZE),
		exitCh:            make(chan interface{}, 0),
	}
}

func (this *OntologyManager) Start() error {
	err := this.initHeartbeat()
	if err != nil {
		return err
	}
	err = this.initAssetHolderCounts()
	if err != nil {
		return err
	}
	go this.startHeartbeat()
	go this.startUpdateInfo()

	err = this.initTotalSupply()
	if err != nil {
		return err
	}
	err = this.initSyncedEvtBlockHeight()
	if err != nil {
		return err
	}
	err = this.initGenesisBlock()
	if err != nil {
		return err
	}
	go this.startSyncEvtNotify()
	go this.handleEvtNotify()
	return nil
}

func (this *OntologyManager) initTotalSupply() error {
	ontTotal, err := this.ontSdk.Native.Ont.TotalSupply()
	if err != nil {
		return err
	}
	this.ONTTotalSupply = ontTotal
	log4.Info("ONTTotalSupply:%d", ontTotal)
	ongTotal, err := this.ontSdk.Native.Ong.TotalSupply()
	if err != nil {
		return err
	}
	this.ONGTotalSupply = ongTotal
	log4.Info("ONGTotalSupply:%d", ongTotal)

	return nil
}

func (this *OntologyManager) initSyncedEvtBlockHeight() error {
	return this.updateSyncedEvtNotifyBlockHeight()
}

func (this *OntologyManager) initAssetHolderCounts() error {
	counts, err := this.mysqlHelper.GetAssetHolderCounts()
	if err != nil {
		return fmt.Errorf("GetAssetHolderCounts error:%s", err)
	}
	this.setAssetHolderCount(counts)
	for contract, count := range counts {
		log4.Info("AssetHolderCounts contract:%s count:%d", contract, count)
	}
	return nil
}

func (this *OntologyManager) initGenesisBlock() error {
	if this.syncedEvtNotifyBlockHeight > 0 {
		return nil
	}
	isGenesisInit, err := this.mysqlHelper.IsGenesisInit()
	if err != nil {
		return fmt.Errorf("mysqlHelper.IsGenesisInit error:%s", err)
	}
	if isGenesisInit {
		return nil
	}
	evts, err := this.ontSdk.GetSmartContractEventByBlock(0)
	if err != nil {
		return fmt.Errorf("GetSmartContractEventByBlock error:%s", err)
	}
	assetHolders := make([]*AssetHolder, 0, 2)
	txNotifies := make([]*TxEventNotify, 0, 2)
	for _, evt := range evts {
		transfers := this.getTxTransferFromNotify(evt)
		if len(transfers) == 0 {
			continue
		}
		transferEvts := make([][]interface{}, 0, 2)
		for _, transfer := range transfers {
			assetHolders = append(assetHolders, &AssetHolder{
				Contract: transfer.Contract,
				Address:  transfer.To,
				Balance:  transfer.Amount,
			})
			transferEvts = append(transferEvts, []interface{}{NOTIFY_TRANSFER, transfer.From, transfer.To, transfer.Amount})
		}
		notifyJson, err := json.Marshal(transferEvts)
		if err != nil {
			log4.Error("handleEvtNotify json.Marshal notify error:%s", err)
			continue
		}
		txNotifies = append(txNotifies, &TxEventNotify{
			TxHash:      evt.TxHash,
			Height:      0,
			State:       int(evt.State),
			GasConsumed: int(evt.GasConsumed),
			Notify:      string(notifyJson),
		})
	}
	err = this.mysqlHelper.OnTxEventNotify(txNotifies, assetHolders)
	if err != nil {
		return fmt.Errorf("OnTxEventNotify error:%s", err)
	}
	return nil
}

func (this *OntologyManager) startSyncEvtNotify() {
	syncEvtTimer := time.NewTimer(time.Second)
	for {
		select {
		case <-syncEvtTimer.C:
			this.syncEvtNotify()
			syncEvtTimer.Reset(time.Second)
		case <-this.exitCh:
			return
		}
	}
}

func (this *OntologyManager) syncEvtNotify() {
	currentBlockHeight, err := this.ontSdk.GetCurrentBlockHeight()
	if err != nil {
		log4.Error("GetCurrentBlockHeight error:%s", err)
		return
	}
	syncedBlockHeight := this.GetSyncedEvtNotifyBlockHeight()
	if currentBlockHeight == syncedBlockHeight {
		return
	}
	log4.Debug("Start to sync block height:%d", syncedBlockHeight+1)
	for height := syncedBlockHeight + 1; uint32(height) <= currentBlockHeight; height++ {
		if this.GetCurrentNodeId() != NodeId {
			return
		}
		evt, err := this.ontSdk.GetSmartContractEventByBlock(uint32(height))
		if err != nil {
			log4.Error("GetSmartContractEventByBlock error:%s", err)
			return
		}
		select {
		case this.syncEvtNotifyChan <- &EventNotify{
			BlockHeight:   uint32(height),
			EventNotifies: evt,
		}:
			this.SetSyncedEvtNotifyBlockHeight(height)
		default:
			return
		}
	}
}

func (this *OntologyManager) getTxTransferFromNotify(txEvt *sdkcom.SmartContactEvent) []*TxTransfer {
	if len(txEvt.Notify) == 0 {
		return nil
	}
	txTransfers := make([]*TxTransfer, 0, 2)
	for _, notify := range txEvt.Notify {
		if notify.ContractAddress != ONT_CONTRACT_ADDRESS && notify.ContractAddress != ONG_CONTRACT_ADDRESS {
			continue
		}
		states, ok := notify.States.([]interface{})
		if !ok {
			continue
		}
		if len(states) != 4 {
			continue
		}
		if states[0] != NOTIFY_TRANSFER {
			continue
		}
		transferFrom, ok := states[1].(string)
		if !ok {
			continue
		}
		transferTo, ok := states[2].(string)
		if !ok {
			continue
		}
		transferAmount, ok := states[3].(uint64)
		if !ok {
			continue
		}

		txTransfers = append(txTransfers, &TxTransfer{
			TxHash:   txEvt.TxHash,
			Contract: notify.ContractAddress,
			From:     SystemContractAddressTransfer(transferFrom),
			To:       SystemContractAddressTransfer(transferTo),
			Amount:   transferAmount,
		})
	}
	return txTransfers
}

func (this *OntologyManager) handleEvtNotify() {
	dbBatchSize := DefConfig.DBBatchSize
	dbBatchTime := time.Duration(DefConfig.DBBatchTime) * time.Second
	txEvtNotifies := make([]*TxEventNotify, 0, dbBatchSize)
	txTransfers := make([]*TxTransfer, 0, dbBatchSize*2)
	notifyTimer := time.NewTimer(dbBatchTime)
	for {
		select {
		case evtNotify := <-this.syncEvtNotifyChan:
			ontEvtNotifies := evtNotify.EventNotifies
			for _, ontEvt := range ontEvtNotifies {
				transfers := this.getTxTransferFromNotify(ontEvt)
				if len(transfers) == 0 {
					continue
				}
				txTransfers = append(txTransfers, transfers...)

				transferEvts := make([][]interface{}, 0, 2)
				for _, transfer := range transfers {
					transferEvts = append(transferEvts, []interface{}{NOTIFY_TRANSFER, transfer.From, transfer.To, transfer.Amount})
				}
				notifyJson, err := json.Marshal(transferEvts)
				if err != nil {
					log4.Error("handleEvtNotify json.Marshal notify error:%s", err)
					continue
				}
				txEvtNotify := &TxEventNotify{
					TxHash:      ontEvt.TxHash,
					Height:      evtNotify.BlockHeight,
					State:       int(ontEvt.State),
					GasConsumed: int(ontEvt.GasConsumed),
					Notify:      string(notifyJson),
				}
				txEvtNotifies = append(txEvtNotifies, txEvtNotify)
				log4.Info("EventNotify:%+v", txEvtNotify)

				if len(txEvtNotifies) >= int(dbBatchSize) {
					this.retryOnTransfer(txEvtNotifies, txTransfers)
					txEvtNotifies = make([]*TxEventNotify, 0, dbBatchSize)
					txTransfers = make([]*TxTransfer, 0, dbBatchSize*2)
					notifyTimer.Reset(dbBatchTime)
				}
			}
		case <-notifyTimer.C:
			if len(txEvtNotifies) > 0 {
				this.retryOnTransfer(txEvtNotifies, txTransfers)
				txEvtNotifies = make([]*TxEventNotify, 0, dbBatchSize)
				txTransfers = make([]*TxTransfer, 0, dbBatchSize*2)
			}
			notifyTimer.Reset(dbBatchTime)
		case <-this.exitCh:
			return
		}
	}
}

func (this *OntologyManager) retryOnTransfer(txNotifies []*TxEventNotify, txTransfers []*TxTransfer) {
	for {
		err := this.onTransfer(txNotifies, txTransfers)
		if err == nil {
			return
		}
		log4.Error("OntologyManager onTransfer error:%s", err)
		select {
		case <-this.exitCh:
			return
		case <-time.After(time.Second):
		}
	}
}

func (this *OntologyManager) onTransfer(txNotifies []*TxEventNotify, txTransfers []*TxTransfer) error {
	if this.GetCurrentNodeId() != NodeId {
		return nil
	}
	txNotifySize := len(txNotifies)
	if txNotifySize == 0 {
		return nil
	}
	txHashes := make([]string, 0, txNotifySize)
	for _, txNotify := range txNotifies {
		txHashes = append(txHashes, txNotify.TxHash)
	}

	isExists, err := this.mysqlHelper.IsEventNotifyExist(txHashes)
	if err != nil {
		return fmt.Errorf("IsEventNotifyExist error:%s", err)
	}

	isExistSize := len(isExists)
	if isExistSize == txNotifySize {
		//All of them has already processed
		return nil
	}
	if isExistSize > 0 {
		size := txNotifySize - isExistSize
		newTxNotifies := make([]*TxEventNotify, 0, size)
		newTxTransfers := make([]*TxTransfer, 0, size*2)
		for _, txNotify := range txNotifies {
			_, ok := isExists[txNotify.TxHash]
			if ok {
				continue
			}
			newTxNotifies = append(newTxNotifies, txNotify)
		}
		for _, txTransfer := range txTransfers {
			_, ok := isExists[txTransfer.TxHash]
			if ok {
				continue
			}
			newTxTransfers = append(newTxTransfers, txTransfer)
		}
		txNotifies = newTxNotifies
		txTransfers = newTxTransfers
	}
	assetHolderKeyMap := make(map[string]bool, txNotifySize)
	assetHolders := make([]*AssetHolder, 0, txNotifySize)
	for _, txTransfer := range txTransfers {
		key := txTransfer.From + txTransfer.Contract
		_, ok := assetHolderKeyMap[key]
		if !ok {
			assetHolderKeyMap[key] = true
			assetHolders = append(assetHolders, &AssetHolder{
				Address:  txTransfer.From,
				Contract: txTransfer.Contract,
			})
		}
		key = txTransfer.To + txTransfer.Contract
		_, ok = assetHolderKeyMap[key]
		if !ok {
			assetHolderKeyMap[key] = true
			assetHolders = append(assetHolders, &AssetHolder{
				Address:  txTransfer.To,
				Contract: txTransfer.Contract,
			})
		}
	}
	assetHolderMap, err := this.mysqlHelper.GetAssetHolderByKey(assetHolders)
	if err != nil {
		return fmt.Errorf("GetAssetHolderByKey error:%s", err)
	}

	for _, txTransfer := range txTransfers {
		key := txTransfer.From + txTransfer.Contract
		assetHolder, ok := assetHolderMap[key]
		if !ok || assetHolder.Balance < txTransfer.Amount {
			err = fmt.Errorf("invalid transfer, Contact:%s TxHash:%s From:%s To:%s Amount:%d", txTransfer.Contract, txTransfer.TxHash, txTransfer.From, txTransfer.To, txTransfer.Amount)
			log4.Error(err)
			time.Sleep(time.Second) //wait to log
			panic(err)
		}
		assetHolder.Balance -= txTransfer.Amount
		assetHolderMap[key] = assetHolder

		key = txTransfer.To + txTransfer.Contract
		assetHolder, ok = assetHolderMap[key]
		if !ok {
			assetHolder = &AssetHolder{
				Contract: txTransfer.Contract,
				Address:  txTransfer.To,
				Balance:  0,
			}
		}
		assetHolder.Balance += txTransfer.Amount
		assetHolderMap[key] = assetHolder
	}

	assetHolders = make([]*AssetHolder, 0, len(assetHolderMap))
	for _, assHolder := range assetHolderMap {
		assetHolders = append(assetHolders, assHolder)
	}

	err = this.mysqlHelper.OnTxEventNotify(txNotifies, assetHolders)
	if err != nil {
		return fmt.Errorf("OnTxEventNotify error:%s", err)
	}
	return nil
}

func (this *OntologyManager) GetOntSdk() *ontsdk.OntologySdk {
	return this.ontSdk
}

func (this *OntologyManager) GetAssetHolder(from, count int, address, contract string) ([]*AssetHolder, error) {
	return this.mysqlHelper.GetAssetHolder(from, count, address, contract)
}

func (this *OntologyManager) GetSyncedEvtNotifyBlockHeight() uint32 {
	return atomic.LoadUint32(&this.syncedEvtNotifyBlockHeight)
}

func (this *OntologyManager) SetSyncedEvtNotifyBlockHeight(height uint32) {
	atomic.StoreUint32(&this.syncedEvtNotifyBlockHeight, height)
}

func (this *OntologyManager) initHeartbeat() error {
	heartbeat, err := this.mysqlHelper.GetHeartbeat(HEARTBEAT_MODULE)
	if err != nil {
		return fmt.Errorf("GetHeartbeat error:%s", err)
	}
	if heartbeat == nil {
		heartbeat = &Heartbeat{
			Module: HEARTBEAT_MODULE,
			NodeId: NodeId,
		}
		err = this.mysqlHelper.InsertHeartbeat(heartbeat)
		if err != nil {
			return fmt.Errorf("InsertHeartbeat error:%s", err)
		}
	}
	this.hb = heartbeat
	return this.heartbeat()
}

func (this *OntologyManager) startHeartbeat() {
	hbInterval := DefConfig.GetHeartbeatUpdateInterval()
	hbTimer := time.NewTimer(time.Duration(hbInterval) * time.Second)
	for {
		select {
		case <-hbTimer.C:
			err := this.heartbeat()
			if err != nil {
				log4.Error("heartbeat error:%s", err)
			}
			hbTimer.Reset(time.Duration(hbInterval) * time.Second)
		case <-this.exitCh:
			return
		}
	}
}

func (this *OntologyManager) heartbeat() error {
	if this.GetCurrentNodeId() == NodeId {
		ok, err := this.mysqlHelper.UpdateHeartbeat(HEARTBEAT_MODULE, NodeId)
		if err != nil {
			return fmt.Errorf("UpdateHeartbeat error:%s", err)
		}
		log4.Debug("UpdateHeartbeat result:%v", ok)
		if ok {
			return nil
		}
		//Node was been switched from current node.
		heartbeat, err := this.mysqlHelper.GetHeartbeat(HEARTBEAT_MODULE)
		if err != nil {
			return fmt.Errorf("GetHeartbeat error:%s", err)
		}
		this.SetCurrentNodeId(heartbeat.NodeId)
		log4.Info("Current node switch to:%d", heartbeat.NodeId)
		return nil
	}
	lastNodeId, err := this.mysqlHelper.CheckHeartbeatTimeout(HEARTBEAT_MODULE, DefConfig.GetHeartbeatTimeoutTime())
	if err != nil {
		return fmt.Errorf("OntologyManager CheckHeartbeatTimeout error:%s", err)
	}
	log4.Debug("CheckHeartbeatTimeout lastNodeId:%d", lastNodeId)
	if lastNodeId == 0 {
		return nil //heartbeat ok
	}
	log4.Info("Current node:%d heartbeat timeout", lastNodeId)
	//heartbeat timeout
	ok, err := this.mysqlHelper.ResetHeartbeat(HEARTBEAT_MODULE, NodeId, lastNodeId)
	if err != nil {
		return fmt.Errorf("OntologyManager ResetHeartbeat error:%s", err)
	}
	if !ok {
		//reset failed
		return nil
	}
	this.SetCurrentNodeId(NodeId)
	log4.Info("NodeId:%d Switch to current node", NodeId)
	return nil
}

func (this *OntologyManager) startUpdateInfo() {
	syncedBlockTime := time.Duration(DefConfig.GetSyncedBlockHeightInterval()) * time.Second
	holderCountTime := time.Duration(DefConfig.GetHolderCountUpdateInterval()) * time.Second

	syncedHeightUpdateTimer := time.NewTimer(syncedBlockTime)
	holderCountUpdateTimer := time.NewTimer(holderCountTime)
	for {
		select {
		case <-syncedHeightUpdateTimer.C:
			if this.GetCurrentNodeId() != NodeId {
				err := this.updateSyncedEvtNotifyBlockHeight()
				if err != nil {
					log4.Error("updateSyncedEvtNotifyBlockHeight error:%s", err)
				}
			}
			syncedHeightUpdateTimer.Reset(syncedBlockTime)
		case <-holderCountUpdateTimer.C:
			err := this.updateAssetHolderCounts()
			if err != nil {
				log4.Error("updateAssetHolderCounts error:%s", err)
			}
			holderCountUpdateTimer.Reset(holderCountTime)
		case <-this.exitCh:
			return
		}
	}
}

func (this *OntologyManager) updateSyncedEvtNotifyBlockHeight() error {
	syncedBlockHeight, err := this.mysqlHelper.GetSyncedEventNotifyBlockHeight()
	if err != nil {
		return fmt.Errorf("GetSyncedEventNotifyBlockHeight error:%s", err)
	}
	if syncedBlockHeight > 0 {
		//Insure all of the block transactions has already inserted to db
		syncedBlockHeight--
	}
	this.SetSyncedEvtNotifyBlockHeight(syncedBlockHeight)
	log4.Info("CurrentSyncedBlockHeight:%d", syncedBlockHeight)
	return nil
}

func (this *OntologyManager) GetCurrentNodeId() uint32 {
	this.lock.RLock()
	defer this.lock.RUnlock()
	return this.hb.NodeId
}

func (this *OntologyManager) SetCurrentNodeId(nodeId uint32) {
	this.lock.Lock()
	defer this.lock.Unlock()
	this.hb.NodeId = nodeId
}

func (this *OntologyManager) updateAssetHolderCounts() error {
	counts, err := this.mysqlHelper.GetAssetHolderCounts()
	if err != nil {
		return fmt.Errorf("GetAssetHolderCounts error:%s", err)
	}
	this.setAssetHolderCount(counts)
	log4.Debug("GetAssetHolderCounts:")
	for contract, count := range counts {
		log4.Debug("Contract:%s count:%d", contract, count)
	}
	return nil
}

func (this *OntologyManager) setAssetHolderCount(counts map[string]int) {
	this.lock.Lock()
	defer this.lock.Unlock()
	this.holderCounts = counts
}

func (this *OntologyManager) GetAssetHolderCount(contract string) int {
	this.lock.RLock()
	defer this.lock.RUnlock()
	if this.holderCounts == nil {
		return 0
	}
	return this.holderCounts[contract]
}

func (this *OntologyManager) Close() {
	close(this.exitCh)
}
