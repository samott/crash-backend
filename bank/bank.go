package bank;

import (
	"errors"
	"database/sql"
	"github.com/shopspring/decimal"
);

type Bank struct {
	db *sql.DB;
};

func NewBank(db *sql.DB) (Bank, error) {
	return Bank{
		db: db,
	}, nil;
}

func (bank *Bank) reduceBalance(
	wallet string,
	currency string,
	amount decimal.Decimal,
) (decimal.Decimal, error) {
	amountStr := amount.String();

	result, err := bank.db.Exec(`
		UPDATE balances
		SET balance = balance - ?
		WHERE wallet = ?
		AND currency = ?
		AND (balance - ?) >= 0
	`, amountStr, wallet, currency, amountStr);

	if err != nil {
		return decimal.Zero, err;
	}

	if rows, err := result.RowsAffected(); rows == 0 || err != nil {
		return decimal.Zero, errors.New("Unable to reduce balance");
	}

	return bank.getBalance(wallet, currency);
}

func (bank *Bank) increaseBalance(
	wallet string,
	currency string,
	amount decimal.Decimal,
) (decimal.Decimal, error) {
	amountStr := amount.String();

	result, err := bank.db.Exec(`
		UPDATE balances
		SET balance = balance + ?
		WHERE wallet = ?
		AND currency = ?
	`, amountStr, wallet, currency);

	if err != nil {
		return decimal.Zero, err;
	}

	if rows, err := result.RowsAffected(); rows == 0 || err != nil {
		return decimal.Zero, errors.New("Unable to increase balance");
	}

	return bank.getBalance(wallet, currency);
}

func (bank *Bank) getBalance(
	wallet string,
	currency string,
) (decimal.Decimal, error) {
	var balanceStr string;

	rows, err := bank.db.Query(`
		SELECT balance FROM balances
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
