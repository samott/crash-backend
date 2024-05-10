package bank;

import (
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
);

type Bank struct {
	db *sql.DB;
};

func NewBank(db *sql.DB) (*Bank, error) {
	return &Bank{
		db: db,
	}, nil;
}

func (bank *Bank) DecreaseBalance(
	wallet string,
	currency string,
	amount decimal.Decimal,
	reason string,
	gameId uuid.UUID,
) (decimal.Decimal, error) {
	amountStr := amount.String();
	amountNegStr := amount.Neg().String();

	tx, err := bank.db.BeginTx(nil, nil);

	if err != nil {
		return decimal.Zero, err;
	}

	defer tx.Rollback();

	result, err := tx.Exec(`
		UPDATE balances
		SET spent = spent + ?
		WHERE wallet = ?
		AND currency = ?
		AND (balance - spent - ?) >= 0
	`, amountStr, wallet, currency, amountStr);

	if err != nil {
		return decimal.Zero, err;
	}

	if rows, err := result.RowsAffected(); rows == 0 || err != nil {
		return decimal.Zero, errors.New("Unable to decrease balance");
	}

	result, err = tx.Exec(`
		INSERT INTO ledger
		(wallet, currency, change, reason, gameId)
		VALUES
		(?, ?, ?, ?)
	`, wallet, currency, amountNegStr, reason, gameId.String());

	if err := tx.Commit(); err != nil {
		return decimal.Zero, nil;
	}

	return bank.GetBalance(wallet, currency);
}

func (bank *Bank) IncreaseBalance(
	wallet string,
	currency string,
	amount decimal.Decimal,
	reason string,
	gameId uuid.UUID,
) (decimal.Decimal, error) {
	amountStr := amount.String();

	tx, err := bank.db.BeginTx(nil, nil);

	if err != nil {
		return decimal.Zero, err;
	}

	defer tx.Rollback();

	result, err := tx.Exec(`
		UPDATE balances
		SET gained = gained + ?
		WHERE wallet = ?
		AND currency = ?
	`, amountStr, wallet, currency);

	if err != nil {
		return decimal.Zero, err;
	}

	if rows, err := result.RowsAffected(); rows == 0 || err != nil {
		return decimal.Zero, errors.New("Unable to increase balance");
	}

	result, err = tx.Exec(`
		INSERT INTO ledger
		(wallet, currency, change, reason, gameId)
		VALUES
		(?, ?, ?, ?)
	`, wallet, currency, amountStr, reason, gameId.String());

	if err := tx.Commit(); err != nil {
		return decimal.Zero, nil;
	}

	return bank.GetBalance(wallet, currency);
}

func (bank *Bank) GetBalance(
	wallet string,
	currency string,
) (decimal.Decimal, error) {
	var balanceStr string;

	rows, err := bank.db.Query(`
		SELECT balance + gained - spent AS balance
		FROM balances
		WHERE wallet = ?
		AND currency = ?
		LIMIT 1
	`, wallet, currency);

	if err != nil {
		return decimal.Zero, err;
	}

	defer rows.Close();

	if (!rows.Next()) {
		return decimal.Zero, errors.New("Balance record not found");
	}

	rows.Scan(&balanceStr);

	balance, err := decimal.NewFromString(balanceStr);

	if err != nil {
		return decimal.Zero, err;
	}

	return balance, nil;
}
