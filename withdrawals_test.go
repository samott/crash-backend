package main

import (
	"log"
	"testing"

	"github.com/samott/crash-backend/config"

	"github.com/shopspring/decimal"
)

var cfg *config.CrashConfig;

func init() {
	var err error;

	cfg, err = config.LoadConfig("./crash_test.yaml");

	if err != nil {
		log.Fatal("failed to load config", err);
	}

	if err != nil {
		log.Fatal("failed to connect to database", "error", err)
		return;
	}

	if err != nil {
		log.Fatal("bank construction failed", err);
	}
}

func TestWithdrawal(t *testing.T) {
	amount, _ := decimal.NewFromString("1");

	_, sig, err := createWithdrawalRequest(
		"0x1111111111111111111111111111111111111111",
		amount,
		"eth",
		1,
		0,
		cfg,
	);

	if err != nil {
		log.Fatal("Failed to create withdrawal request: ", err);
	}

	expectedSig := "0x4aaa23c780b7c15b65bb33e283b7e3be3b364b61bfe1e02060d63cf2cfc6edf56f6e1f8312dcf9323239637b03bf5f89c9a80a6967e20da0a6b8e9a43fbaa0011b";

	if sig != expectedSig {
		log.Fatal("Incorrect signature for withdrawal request: ", sig, " vs. ", expectedSig);
	}
}
