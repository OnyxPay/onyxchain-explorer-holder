package main

import (
	"bytes"
	"database/sql"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	log4 "github.com/alecthomas/log4go"
	_ "github.com/go-sql-driver/mysql"
)

type MySqlHelper struct {
	MySqlAddress         string
	MySqlUserName        string
	MySqlPassword        string
	MySqlDBName          string
	MySqlMaxIdleConnSize uint32
	MySqlMaxOpenConnSize uint32
	MySqlConnMaxLifetime uint32
	db                   *sql.DB
	hide                 string
}

func NewMySqlHelper(address, username, passwd, dbName string, maxIdleConnSize, maxOpenConnSize, connMaxLiftTime uint32) *MySqlHelper {
	return &MySqlHelper{
		MySqlAddress:         address,
		MySqlUserName:        username,
		MySqlPassword:        passwd,
		MySqlDBName:          dbName,
		MySqlMaxIdleConnSize: maxIdleConnSize,
		MySqlMaxOpenConnSize: maxOpenConnSize,
		MySqlConnMaxLifetime: connMaxLiftTime,
	}
}

func (this *MySqlHelper) Open() error {
	dsn := fmt.Sprintf("%s:%s@tcp(%s)/%s?charset=utf8", this.MySqlUserName, this.MySqlPassword, this.MySqlAddress, this.MySqlDBName)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return err
	}
	db.SetMaxIdleConns(int(this.MySqlMaxIdleConnSize))
	db.SetMaxOpenConns(int(this.MySqlMaxOpenConnSize))
	db.SetConnMaxLifetime(time.Duration(this.MySqlConnMaxLifetime) * time.Second)
	err = db.Ping()
	if err != nil {
		return err
	}
	this.db = db
	this.hide = os.Getenv("HIDE_ADDRESSES")
	return nil
}

func (this *MySqlHelper) Close() error {
	return this.db.Close()
}

func (this *MySqlHelper) InitDB(installFile string) error {
	isTableCreate, err := this.isTableCreate()
	if err != nil {
		return err
	}
	if !isTableCreate {
		err = this.createTable(installFile)
		if err != nil {
			return err
		}
	}
	return nil
}

func (this *MySqlHelper) isTableCreate() (bool, error) {
	sqlText := "SELECT count(*) FROM information_schema.TABLES WHERE table_name ='holder' And table_schema ='" + this.MySqlDBName + "';"
	rows, err := this.db.Query(sqlText)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	count := 0
	rows.Next()
	err = rows.Scan(&count)
	if err != nil {
		return false, err
	}
	return count != 0, nil
}

func (this *MySqlHelper) createTable(installFile string) error {
	data, err := ioutil.ReadFile(installFile)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("createTable file empty")
	}

	dbTx, err := this.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction error:%s", err)
	}
	rollBack := true
	defer func() {
		if rollBack {
			e := dbTx.Rollback()
			if e != nil {
				log4.Error("dbTx Rollback error %s", err)
			}
		}
	}()

	sqlTexts := strings.Split(string(data), ";")
	for _, sqlText := range sqlTexts {
		sqlText = strings.TrimSpace(sqlText)
		if sqlText == "" {
			continue
		}
		_, err = dbTx.Exec(sqlText)
		if err != nil {
			return fmt.Errorf("install table failed, exec:%s error:%s", sqlText, err)
		}
		log4.Info("CreateTable:%s success.", sqlText)
	}

	err = dbTx.Commit()
	if err != nil {
		return fmt.Errorf("intall dbTx.Commit error:%s", err)
	}
	rollBack = false
	return nil
}

func (this *MySqlHelper) OnTxEventNotify(evtNotify []*TxEventNotify, assetHolder []*AssetHolder) error {
	notifyCount := len(evtNotify)
	if notifyCount == 0 {
		return nil
	}
	notifySqlBuf := bytes.NewBuffer(nil)
	notifySqlBuf.WriteString("Insert Into eventnotify(tx_hash, height, state, gas_consumed, notify) Values ")
	for i, notify := range evtNotify {
		notifySqlBuf.WriteString(fmt.Sprintf("('%s', %d, %d, %d ,'%s')", notify.TxHash, notify.Height, notify.State, notify.GasConsumed, notify.Notify))
		if i == notifyCount-1 {
			notifySqlBuf.WriteString(";")
		} else {
			notifySqlBuf.WriteString(",")
		}
	}
	notifySqlText := notifySqlBuf.String()

	holderCount := len(assetHolder)
	if holderCount == 0 {
		return nil
	}
	holderSqlBuf := bytes.NewBuffer(nil)
	holderSqlBuf.WriteString("Insert Into holder(address, contract, balance) Values ")
	for i, holder := range assetHolder {
		holderSqlBuf.WriteString(fmt.Sprintf("('%s', '%s', %d)", holder.Address, holder.Contract, holder.Balance))
		if i == holderCount-1 {
			holderSqlBuf.WriteString(" On Duplicate key Update balance=Values(balance);")
		} else {
			holderSqlBuf.WriteString(",")
		}
	}
	holderSqlText := holderSqlBuf.String()
	dbTx, err := this.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction error:%s", err)
	}
	rollBack := true
	defer func() {
		if rollBack {
			e := dbTx.Rollback()
			if e != nil {
				log4.Error("OnTxEventNotify dbTx Rollback error %s", err)
			}
		}
	}()

	results, err := dbTx.Exec(notifySqlText)
	if err != nil {
		return fmt.Errorf("insert notify dbTx.Exec error:%s", err)
	}
	affected, err := results.RowsAffected()
	if err != nil {
		return fmt.Errorf("insert notify dbTx.Exec RowsAffected error:%s", err)
	}
	if int(affected) != notifyCount {
		fmt.Printf("Insert notify dbTx.Exec  RowsAffected %d != %d\n", affected, notifyCount)
		return nil
	}
	_, err = dbTx.Exec(holderSqlText)
	if err != nil {
		return fmt.Errorf("insert holder dbTx.Exec error:%s", err)
	}

	err = dbTx.Commit()
	if err != nil {
		return fmt.Errorf("OnTxEventNotify dbTx.Commit error:%s", err)
	}
	rollBack = false
	return nil
}

func (this *MySqlHelper) GetAssetHolder(from, count int, address, contract string, isDescOrder ...bool) ([]*AssetHolder, error) {
	order := "DESC"
	if len(isDescOrder) > 0 && !isDescOrder[0] {
		order = "ASC"
	}
	buf := bytes.NewBuffer(nil)
	if contract != "" {
		buf.WriteString("Select address, balance From holder Where contract = '" + contract + "' ")
	} else {
		buf.WriteString("Select address, contract, balance From holder Where ")
	}
	if address != "" {
		if contract != "" {
			buf.WriteString("And ")
		}
		buf.WriteString("address = '" + address + "' ")
	} else if this.hide != "" {
		if contract != "" {
			buf.WriteString("And ")
		}
		buf.WriteString("address not in (" + this.hide + ")")
	}
	buf.WriteString("Order By balance " + order)
	if count == 0 {
		buf.WriteString(";")
	} else {
		buf.WriteString(fmt.Sprintf(" Limit %d, %d;", from, count))
	}
	sqlText := buf.String()
	log4.Debug("GetAssetHolder SqlText:%s", sqlText)

	rows, err := this.db.Query(sqlText)
	if err != nil {
		return nil, fmt.Errorf("db.Query error:%s", err)
	}
	defer rows.Close()

	holders := make([]*AssetHolder, 0, count)
	for rows.Next() {
		holder := &AssetHolder{}
		if contract != "" {
			err = rows.Scan(&holder.Address, &holder.Balance)
			holder.Contract = contract
		} else {
			err = rows.Scan(&holder.Address, &holder.Contract, &holder.Balance)
		}
		if err != nil {
			return nil, fmt.Errorf("row.Scan error:%s", err)
		}
		holders = append(holders, holder)
	}
	return holders, nil
}

func (this *MySqlHelper) GetSyncedEventNotifyBlockHeight() (uint32, error) {
	sqlText := "Select ifnull(max(height),0) From eventnotify;"
	rows, err := this.db.Query(sqlText)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	syncedHeight := uint32(0)
	if !rows.Next() {
		return syncedHeight, nil
	}
	err = rows.Scan(&syncedHeight)
	if err != nil {
		return syncedHeight, fmt.Errorf("row.Scan error:%s", err)
	}
	return syncedHeight, nil
}

func (this *MySqlHelper) IsGenesisInit() (bool, error) {
	sqlText := "Select ifnull(count(balance),0) From holder;"
	rows, err := this.db.Query(sqlText)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, nil
	}
	count := 0
	err = rows.Scan(&count)
	if err != nil {
		return false, fmt.Errorf("row.Scan error:%s", err)
	}
	return count > 0, nil
}

func (this *MySqlHelper) IsEventNotifyExist(txHashes []string) (map[string]bool, error) {
	count := len(txHashes)
	if count == 0 {
		return nil, nil
	}
	sqlBuf := bytes.NewBuffer(nil)
	sqlBuf.WriteString("Select tx_hash From eventnotify Where tx_hash In (")
	for i, txHash := range txHashes {
		sqlBuf.WriteString("'" + txHash + "'")
		if i == count-1 {
			sqlBuf.WriteString(");")
		} else {
			sqlBuf.WriteString(", ")
		}
	}
	rows, err := this.db.Query(sqlBuf.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	txHashMap := make(map[string]bool, 0)
	for rows.Next() {
		txHash := ""
		err = rows.Scan(&txHash)
		if err != nil {
			return nil, fmt.Errorf("row.Scan error:%s", err)
		}
		txHashMap[txHash] = true
	}
	return txHashMap, nil
}

func (this *MySqlHelper) GetAssetHolderByKey(holders []*AssetHolder) (map[string]*AssetHolder, error) {
	count := len(holders)
	if count == 0 {
		return nil, nil
	}
	sqlBuf := bytes.NewBuffer(nil)
	sqlBuf.WriteString("Select address, contract, balance From holder Where ")
	for i, holder := range holders {
		if i == count-1 {
			sqlBuf.WriteString("(address='" + holder.Address + "' And contract='" + holder.Contract + "');")
		} else {
			sqlBuf.WriteString("(address='" + holder.Address + "' And contract='" + holder.Contract + "') Or")
		}
	}
	sqlText := sqlBuf.String()
	rows, err := this.db.Query(sqlText)
	if err != nil {
		return nil, fmt.Errorf("db.Query error:%s", err)
	}
	defer rows.Close()

	holderMap := make(map[string]*AssetHolder, count)
	for rows.Next() {
		holder := &AssetHolder{}
		err = rows.Scan(&holder.Address, &holder.Contract, &holder.Balance)
		if err != nil {
			return nil, fmt.Errorf("row.Scan error:%s", err)
		}
		holderMap[holder.Address+holder.Contract] = holder
	}
	return holderMap, nil
}

func (this *MySqlHelper) GetAssetHolderCount(contract string) (int, error) {
	sqlText := "Select ifnull(count(balance),0) From holder Where contract = '" + contract + "';"
	rows, err := this.db.Query(sqlText)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, nil
	}
	count := 0
	err = rows.Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("row.Scan error:%s", err)
	}
	return count, nil
}

func (this *MySqlHelper) GetAssetHolderCounts() (map[string]int, error) {
	sqlText := "Select contract, count(*) From holder group by contract;"
	rows, err := this.db.Query(sqlText)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[string]int)
	for rows.Next() {
		contract := ""
		count := 0
		err = rows.Scan(&contract, &count)
		if err != nil {
			return nil, fmt.Errorf("row.Scan error:%s", err)
		}
		counts[contract] = count
	}
	return counts, nil
}

func (this *MySqlHelper) GetHeartbeat(module string) (*Heartbeat, error) {
	sqlText := "Select node_id, update_time From heartbeat Where module = '" + module + "'"
	rows, err := this.db.Query(sqlText)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, nil
	}
	heartbeat := &Heartbeat{Module: module}
	err = rows.Scan(&heartbeat.NodeId, &heartbeat.UpdateTime)
	if err != nil {
		return nil, fmt.Errorf("row scan error:%s", err)
	}
	return heartbeat, nil
}

func (this *MySqlHelper) InsertHeartbeat(heartbeat *Heartbeat) error {
	sqlText := fmt.Sprintf("Insert into heartbeat(module, node_id, update_time) Values ('%s', %d, Now());", heartbeat.Module, heartbeat.NodeId)
	results, err := this.db.Exec(sqlText)
	if err != nil {
		return fmt.Errorf("db.Exec error:%s", err)
	}
	affected, err := results.RowsAffected()
	if err != nil {
		return fmt.Errorf("RowsAffected() error:%s", err)
	}
	if affected != 1 {
		return fmt.Errorf("affected != 1")
	}
	return nil
}

func (this *MySqlHelper) UpdateHeartbeat(module string, nodeId uint32) (bool, error) {
	sqlText := fmt.Sprintf("Update heartbeat Set update_time = Now() Where node_id = %d;", nodeId)
	results, err := this.db.Exec(sqlText)
	if err != nil {
		return false, fmt.Errorf("db.Exec error:%s", err)
	}
	affected, err := results.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("RowsAffected() error:%s", err)
	}
	if affected != 1 {
		return false, nil
	}
	return true, nil
}

func (this *MySqlHelper) CheckHeartbeatTimeout(module string, timeout uint32) (uint32, error) {
	sqlText := fmt.Sprintf("Select ifnull(node_id,0) From heartbeat Where module = '%s' And (Now()-update_time) >= %d;", module, timeout)
	rows, err := this.db.Query(sqlText)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if !rows.Next() {
		return 0, nil
	}
	nodeId := uint32(0)
	err = rows.Scan(&nodeId)
	if err != nil {
		return 0, fmt.Errorf("row.Scan error:%s", err)
	}
	return nodeId, nil
}

func (this *MySqlHelper) ResetHeartbeat(module string, nodeId, lastNodeId uint32) (bool, error) {
	sqlText := fmt.Sprintf("Update heartbeat Set node_id = %d, update_time = Now() Where module = '%s' And node_id = %d;", nodeId, module, lastNodeId)
	results, err := this.db.Exec(sqlText)
	if err != nil {
		return false, fmt.Errorf("db.Exec error:%s", err)
	}
	affected, err := results.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("RowsAffected() error:%s", err)
	}
	return affected == 1, nil
}
