package main

import (
	"fmt"
	"testing"

	ontsdk "github.com/ontio/ontology-go-sdk"
)

//Warning: before run test, should stop sync block from ontology node.
func TestAssetHolder(t *testing.T) {
	err := GetJsonObject(CfgPath, DefConfig)
	if err != nil {
		t.Errorf("init config error:%s", err)
		return
	}

	ontSdk := ontsdk.NewOntologySdk()
	rpcClient := ontSdk.NewRpcClient().SetAddress(DefConfig.OntologyRpcAddress)
	ontSdk.SetDefaultClient(rpcClient)

	mySqlHelper := NewMySqlHelper(
		DefConfig.MySqlAddress,
		DefConfig.MySqlUserName,
		DefConfig.MySqlPassword,
		DefConfig.MySqlDBName,
		DefConfig.MySqlMaxIdleConnSize,
		DefConfig.MySqlMaxOpenConnSize,
		DefConfig.MySqlConnMaxLifetime)
	err = mySqlHelper.Open()
	if err != nil {
		t.Errorf("mySqlHelper.Open error:%s", err)
		return
	}
	err = mySqlHelper.InitDB(DBInstallFile)
	if err != nil {
		t.Errorf("InitDB error:%s", err)
		return
	}
	contract := ONT_CONTRACT_ADDRESS
	size := 100
	for from := 0; ; from += size {
		assetHolders, err := mySqlHelper.GetAssetHolder(from, size, "", contract)
		if err != nil {
			t.Errorf("GetAssetHolder error:%s", err)
			return
		}
		if len(assetHolders) == 0 {
			break
		}
		fmt.Printf("Test From:%d Count:%d\n", from, size)
		for _, assetHolder := range assetHolders {
			addr, err := GetAddress(assetHolder.Address)
			if err != nil {
				t.Errorf("GetAddress:%s error:%s", addr, err)
				return
			}
			balance, err := ontSdk.Native.Ont.BalanceOf(addr)
			if err != nil {
				t.Errorf("Ont.BalanceOf error:%s", err)
				return
			}
			if balance != assetHolder.Balance {
				fmt.Printf("ONT Address:%s balance:%d != %d\n", assetHolder.Address, assetHolder.Balance, balance)
			}
		}
	}
}
