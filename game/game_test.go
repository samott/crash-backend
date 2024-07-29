package game

import (
	"log"
	"testing"

	"github.com/samott/crash-backend/bank"
	"github.com/samott/crash-backend/config"

	"database/sql"

	"github.com/go-sql-driver/mysql"
	"github.com/shopspring/decimal"
)

var gameObj *Game;

func init() {
	config, err := config.LoadConfig("../crash.yaml");

	if err != nil {
		log.Fatal("failed to load config", err);
	}

	dbConfig := mysql.Config{
		User: config.Database.User,
		DBName: config.Database.DBName,
		Addr: config.Database.Addr,
		AllowNativePasswords: true,
	};

	db, err := sql.Open("mysql", dbConfig.FormatDSN());

	if err != nil {
		log.Fatal("failed to connect to database", "error", err)
		return;
	}

	bankObj, err := bank.NewBank(db);

	if err != nil {
		log.Fatal("bank construction failed", err);
	}

	gameObj, err = NewGame(nil, db, config, nil, Bank(bankObj));

	if err != nil {
		log.Fatal("game construction failed", err);
	}
}

func TestHashCalculations(t *testing.T) {
	seed := "cats_are_everywhere";

	hash := generateGameHash(seed);

	if hash != "a39a59caa7ea909dc72685681062a1bfd650f155ac6018677b0f4de5a0d8430b" {
		t.Fatalf("generateGameHash() result is incorrect: %s", hash);
	}

	multiplier := hashToMultiplier(hash);
	expected, err := decimal.NewFromString("2.71");

	if err != nil {
		t.Fatalf("failed to create decimal value: %s", err);
	}

	if !multiplier.Equal(expected) {
		t.Fatalf("hashToMultiplier() result is incorrect: %s", multiplier);
	}

	duration, err := multiplierToDuration(multiplier);

	if err != nil {
		t.Fatalf("failed to calculate multiplier: %s", err);
	}

	if duration.Milliseconds() != 16615 {
		t.Fatalf("multiplierToDuration() result is incorrect: %d", duration);
	}
}
