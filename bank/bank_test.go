package bank;

import (
	"testing"
	"log"

	"github.com/samott/crash-backend/config"

	"github.com/ethereum/go-ethereum/crypto"

	"database/sql"
	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
	"github.com/google/uuid"
)

var bankObj *Bank;

func init() {
	config, err := config.LoadConfig("../crash.yaml");

	if err != nil {
		log.Fatal("Failed to load config");
	}

	dbConfig := mysql.Config{
		User: config.Database.User,
		DBName: config.Database.DBName,
		Addr: config.Database.Addr,
		AllowNativePasswords: true,
	};

	db, err := sql.Open("mysql", dbConfig.FormatDSN());

	if err != nil {
		log.Fatal("Failed to connect to database", "error", err)
		return;
	}

	bankObj, err = NewBank(db);

	if err != nil {
		log.Fatal("Bank construction failed", err);
	}
}

func TestGetBalance(t *testing.T) {
	randomUser, err := crypto.GenerateKey();
	wallet := crypto.PubkeyToAddress(randomUser.PublicKey).String();

	_, err = bankObj.db.Exec(`
		INSERT INTO balances
		(currency, balance, wallet)
		VALUES
		(?, ?, ?)
	`, "eth", "403", wallet);

	balance, err := bankObj.GetBalance(wallet, "eth");

	if err != nil {
		t.Fatal("Failed to get balance");
	}

	if balance.StringFixed(2) != "403.00" {
		t.Fatal("GetBalance() result is incorrect");
	}
}

func TestIncreaseBalance(t *testing.T) {
	randomUser, err := crypto.GenerateKey();
	wallet := crypto.PubkeyToAddress(randomUser.PublicKey).String();

	_, err = bankObj.db.Exec(`
		INSERT INTO balances
		(currency, balance, wallet)
		VALUES
		(?, ?, ?)
	`, "eth", "100", wallet);

	amount, err := decimal.NewFromString("17.12");

	if err != nil {
		t.Fatal("Failed to create decimal");
	}

	gameId, err := uuid.NewV7();

	if err != nil {
		t.Fatal("Failed to create uuid");
	}

	balance, err := bankObj.IncreaseBalance(wallet, "eth", amount, "Credit", gameId);

	if err != nil {
		t.Fatal("Failed to increase balance");
	}

	if balance.StringFixed(2) != "117.12" {
		t.Fatal("GetBalance() result is incorrect");
	}
}

func TestDecreaseBalance(t *testing.T) {
	randomUser, err := crypto.GenerateKey();
	wallet := crypto.PubkeyToAddress(randomUser.PublicKey).String();

	_, err = bankObj.db.Exec(`
		INSERT INTO balances
		(currency, balance, wallet)
		VALUES
		(?, ?, ?)
	`, "eth", "100", wallet);

	amount, err := decimal.NewFromString("17.12");

	if err != nil {
		t.Fatal("Failed to create decimal");
	}

	gameId, err := uuid.NewV7();

	if err != nil {
		t.Fatal("Failed to create uuid");
	}

	balance, err := bankObj.DecreaseBalance(wallet, "eth", amount, "Credit", gameId);

	if err != nil {
		t.Fatal("Failed to decrease balance");
	}

	if balance.StringFixed(2) != "82.88" {
		t.Fatal("GetBalance() result is incorrect");
	}
}

func TestWithdrawBalance(t *testing.T) {
	randomUser, err := crypto.GenerateKey();
	wallet := crypto.PubkeyToAddress(randomUser.PublicKey).String();

	_, err = bankObj.db.Exec(`
		INSERT INTO balances
		(currency, balance, wallet)
		VALUES
		(?, ?, ?)
	`, "eth", "100", wallet);

	amount, err := decimal.NewFromString("17.16");

	if err != nil {
		t.Fatal("Failed to create decimal");
	}

	balance, err := bankObj.WithdrawBalance(wallet, "eth", amount);

	if err != nil {
		t.Fatal("Failed to withdraw balance");
	}

	if balance.StringFixed(2) != "82.84" {
		t.Fatal("GetBalance() result is incorrect");
	}
}
